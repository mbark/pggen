package ch

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Parse turns a ClickHouse type name into a Type. The names come from
// DESCRIBE, so they are the canonical spellings the server produces, like
// "LowCardinality(Nullable(String))" or "Enum8('MOC' = 1, 'SMO' = 2)".
//
// A type pggen has no Go mapping for still parses, into Unsupported, so the
// caller can report which type it choked on rather than where it stopped
// lexing.
func Parse(name string) (Type, error) {
	p := &parser{src: name}
	t, err := p.parseType()
	if err != nil {
		return nil, err
	}
	p.skipSpace()
	if p.pos != len(p.src) {
		return nil, p.errf("unexpected trailing text %q", p.src[p.pos:])
	}
	return t, nil
}

// MustParse is Parse for type names known at compile time, like the entries in
// a known-type table.
func MustParse(name string) Type {
	t, err := Parse(name)
	if err != nil {
		panic(err.Error())
	}
	return t
}

type parser struct {
	src string
	pos int
}

func (p *parser) errf(format string, args ...any) error {
	return fmt.Errorf("parse clickhouse type %q at offset %d: %s",
		p.src, p.pos, fmt.Sprintf(format, args...))
}

func (p *parser) skipSpace() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t' || p.src[p.pos] == '\n') {
		p.pos++
	}
}

// peek returns the next non-space byte, or 0 at end of input.
func (p *parser) peek() byte {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) accept(c byte) bool {
	if p.peek() == c {
		p.pos++
		return true
	}
	return false
}

func (p *parser) expect(c byte) error {
	if !p.accept(c) {
		return p.errf("expected %q", string(c))
	}
	return nil
}

func isIdentByte(c byte) bool {
	return c == '_' ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9')
}

func (p *parser) parseIdent() (string, error) {
	p.skipSpace()
	start := p.pos
	for p.pos < len(p.src) && isIdentByte(p.src[p.pos]) {
		p.pos++
	}
	if p.pos == start {
		return "", p.errf("expected a type name")
	}
	return p.src[start:p.pos], nil
}

// parseString reads a single-quoted ClickHouse string literal, honouring the
// backslash escapes the server emits inside enum labels and timezone names.
func (p *parser) parseString() (string, error) {
	if err := p.expect('\''); err != nil {
		return "", err
	}
	sb := &strings.Builder{}
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		switch c {
		case '\\':
			if p.pos+1 >= len(p.src) {
				return "", p.errf("trailing backslash in string literal")
			}
			p.pos += 2
			switch p.src[p.pos-1] {
			case 'n':
				sb.WriteByte('\n')
			case 't':
				sb.WriteByte('\t')
			case 'r':
				sb.WriteByte('\r')
			case '0':
				sb.WriteByte(0)
			default:
				sb.WriteByte(p.src[p.pos-1])
			}
		case '\'':
			p.pos++
			return sb.String(), nil
		default:
			sb.WriteByte(c)
			p.pos++
		}
	}
	return "", p.errf("unterminated string literal")
}

func (p *parser) parseInt() (int, error) {
	p.skipSpace()
	start := p.pos
	if p.pos < len(p.src) && (p.src[p.pos] == '-' || p.src[p.pos] == '+') {
		p.pos++
	}
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	if p.pos == start {
		return 0, p.errf("expected a number")
	}
	n, err := strconv.Atoi(p.src[start:p.pos])
	if err != nil {
		return 0, p.errf("bad number %q", p.src[start:p.pos])
	}
	return n, nil
}

// decimalPrecision maps the fixed-width Decimal spellings to the precision
// their width implies. https://clickhouse.com/docs/sql-reference/data-types/decimal
var decimalPrecision = map[string]int{
	"Decimal32": 9, "Decimal64": 18, "Decimal128": 38, "Decimal256": 76,
}

func (p *parser) parseType() (Type, error) {
	openIdx := p.pos
	name, err := p.parseIdent()
	if err != nil {
		return nil, err
	}
	if p.peek() != '(' {
		// A bare name. Two parameterized types have a meaningful argument-less
		// spelling; everything else without arguments is a scalar.
		switch name {
		case "DateTime":
			return DateTime{}, nil
		case "DateTime64":
			// ClickHouse defaults DateTime64 to millisecond precision.
			return DateTime64{Precision: 3}, nil
		}
		return Scalar{Name: name}, nil
	}

	switch name {
	case "Nullable", "LowCardinality", "Array":
		elem, err := p.parseParenType()
		if err != nil {
			return nil, err
		}
		switch name {
		case "Nullable":
			return Nullable{Elem: elem}, nil
		case "LowCardinality":
			return LowCardinality{Elem: elem}, nil
		default:
			return Array{Elem: elem}, nil
		}

	case "Map":
		if err := p.expect('('); err != nil {
			return nil, err
		}
		key, err := p.parseType()
		if err != nil {
			return nil, err
		}
		if err := p.expect(','); err != nil {
			return nil, err
		}
		val, err := p.parseType()
		if err != nil {
			return nil, err
		}
		if err := p.expect(')'); err != nil {
			return nil, err
		}
		return Map{KeyType: key, ValType: val}, nil

	case "FixedString":
		n, err := p.parenInt()
		if err != nil {
			return nil, err
		}
		return FixedString{N: n}, nil

	case "Decimal":
		if err := p.expect('('); err != nil {
			return nil, err
		}
		prec, err := p.parseInt()
		if err != nil {
			return nil, err
		}
		if err := p.expect(','); err != nil {
			return nil, err
		}
		scale, err := p.parseInt()
		if err != nil {
			return nil, err
		}
		if err := p.expect(')'); err != nil {
			return nil, err
		}
		return Decimal{Precision: prec, Scale: scale}, nil

	case "Decimal32", "Decimal64", "Decimal128", "Decimal256":
		scale, err := p.parenInt()
		if err != nil {
			return nil, err
		}
		return Decimal{Precision: decimalPrecision[name], Scale: scale}, nil

	case "DateTime":
		tz, err := p.parenString()
		if err != nil {
			return nil, err
		}
		return DateTime{TZ: tz}, nil

	case "DateTime64":
		if err := p.expect('('); err != nil {
			return nil, err
		}
		prec, err := p.parseInt()
		if err != nil {
			return nil, err
		}
		tz := ""
		if p.accept(',') {
			if tz, err = p.parseString(); err != nil {
				return nil, err
			}
		}
		if err := p.expect(')'); err != nil {
			return nil, err
		}
		return DateTime64{Precision: prec, TZ: tz}, nil

	case "Enum8", "Enum16":
		bits := 8
		if name == "Enum16" {
			bits = 16
		}
		return p.parseEnum(bits)

	case "Tuple":
		return p.parseTuple()
	}

	// Anything else with arguments — Nested, AggregateFunction, and the rest —
	// is kept verbatim so the caller can name it in an error.
	if err := p.skipBalanced(); err != nil {
		return nil, err
	}
	return Unsupported{Raw: p.src[openIdx:p.pos]}, nil
}

// parseParenType reads a single parenthesised type argument.
func (p *parser) parseParenType() (Type, error) {
	if err := p.expect('('); err != nil {
		return nil, err
	}
	t, err := p.parseType()
	if err != nil {
		return nil, err
	}
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	return t, nil
}

// parenInt reads a single parenthesised integer argument.
func (p *parser) parenInt() (int, error) {
	if err := p.expect('('); err != nil {
		return 0, err
	}
	n, err := p.parseInt()
	if err != nil {
		return 0, err
	}
	if err := p.expect(')'); err != nil {
		return 0, err
	}
	return n, nil
}

// parenString reads a single parenthesised string argument, which the caller
// has already established is present.
func (p *parser) parenString() (string, error) {
	if err := p.expect('('); err != nil {
		return "", err
	}
	s, err := p.parseString()
	if err != nil {
		return "", err
	}
	if err := p.expect(')'); err != nil {
		return "", err
	}
	return s, nil
}

func (p *parser) parseEnum(bits int) (Type, error) {
	if err := p.expect('('); err != nil {
		return nil, err
	}
	e := Enum{Bits: bits}
	for {
		label, err := p.parseString()
		if err != nil {
			return nil, err
		}
		if err := p.expect('='); err != nil {
			return nil, err
		}
		v, err := p.parseInt()
		if err != nil {
			return nil, err
		}
		// Enum.Values is int16, so an unchecked Enum16 value would truncate
		// silently and the truncated value would end up in the generated SQL.
		lo, hi := math.MinInt16, math.MaxInt16
		if bits == 8 {
			lo, hi = math.MinInt8, math.MaxInt8
		}
		if v < lo || v > hi {
			return nil, p.errf("Enum%d value %d for label %q is out of range",
				bits, v, label)
		}
		e.Labels = append(e.Labels, label)
		e.Values = append(e.Values, int16(v))
		if !p.accept(',') {
			break
		}
	}
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	return e, nil
}

func (p *parser) parseTuple() (Type, error) {
	if err := p.expect('('); err != nil {
		return nil, err
	}
	t := Tuple{}
	named := false
	for {
		// A named element is "<ident> <type>". Both start with an identifier,
		// so read one and look at what follows to tell them apart.
		save := p.pos
		ident, err := p.parseIdent()
		if err != nil {
			return nil, err
		}
		next := p.peek()
		if next != '(' && next != ',' && next != ')' {
			// Another token follows the identifier, so it was a field name.
			elem, err := p.parseType()
			if err != nil {
				return nil, err
			}
			named = true
			t.Names = append(t.Names, ident)
			t.Elems = append(t.Elems, elem)
		} else {
			p.pos = save
			elem, err := p.parseType()
			if err != nil {
				return nil, err
			}
			t.Names = append(t.Names, "")
			t.Elems = append(t.Elems, elem)
		}
		if !p.accept(',') {
			break
		}
	}
	if err := p.expect(')'); err != nil {
		return nil, err
	}
	if !named {
		t.Names = nil
	}
	return t, nil
}

// skipBalanced consumes a parenthesised group, respecting nesting and string
// literals, leaving pos just past the closing paren.
func (p *parser) skipBalanced() error {
	if err := p.expect('('); err != nil {
		return err
	}
	depth := 1
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case '\'':
			if _, err := p.parseString(); err != nil {
				return err
			}
			continue
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				p.pos++
				return nil
			}
		}
		p.pos++
	}
	return p.errf("unbalanced parentheses")
}
