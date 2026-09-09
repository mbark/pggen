package ch

import (
	"fmt"
	"strings"
)

// Param is a {name:Type} query parameter.
type Param struct {
	Name string
	Type Type
}

// scanSQL walks sql and calls onParam for each {name:Type} it finds, and
// onText for every stretch of SQL between them.
//
// It skips string literals, quoted identifiers, and comments, so a brace
// inside one is not mistaken for a parameter.
func scanSQL(sql string, onText func(string), onParam func(Param) error) error {
	prev := 0
	emitText := func(upto int) {
		if onText != nil && upto > prev {
			onText(sql[prev:upto])
		}
	}
	for i := 0; i < len(sql); i++ {
		switch c := sql[i]; c {
		case '\'', '`', '"':
			// A string literal or quoted identifier.
			quote := c
			for i++; i < len(sql); i++ {
				if sql[i] == '\\' {
					i++
					continue
				}
				if sql[i] == quote {
					break
				}
			}
		case '-':
			if i+1 < len(sql) && sql[i+1] == '-' {
				for i < len(sql) && sql[i] != '\n' {
					i++
				}
			}
		case '/':
			if i+1 < len(sql) && sql[i+1] == '*' {
				i += 2
				for i+1 < len(sql) && (sql[i] != '*' || sql[i+1] != '/') {
					i++
				}
				i++
			}
		case '{':
			end := strings.IndexByte(sql[i:], '}')
			if end < 0 {
				return fmt.Errorf("unclosed { in query; a ClickHouse parameter looks like {name:Type}")
			}
			body := sql[i+1 : i+end]

			colon := strings.IndexByte(body, ':')
			if colon < 0 {
				return fmt.Errorf("query parameter {%s} is missing its type; "+
					"ClickHouse parameters look like {%s:String}", body, strings.TrimSpace(body))
			}
			name := strings.TrimSpace(body[:colon])
			if name == "" {
				return fmt.Errorf("query parameter {%s} is missing its name", body)
			}
			typ, err := Parse(strings.TrimSpace(body[colon+1:]))
			if err != nil {
				return fmt.Errorf("query parameter %q: %w", name, err)
			}

			emitText(i)
			if err := onParam(Param{Name: name, Type: typ}); err != nil {
				return err
			}
			i += end
			prev = i + 1
		}
	}
	emitText(len(sql))
	return nil
}

// ScanParams finds the ClickHouse query parameters in sql, in order of first
// appearance.
//
// This is where ClickHouse differs most from Postgres. Postgres infers a
// parameter's type server-side from where it appears, so pggen never has to be
// told. ClickHouse spells a parameter {name:Type} with the type written by
// hand, which means the type is already in the query text and no round trip is
// needed to learn it.
//
// A parameter may appear more than once; repeats must agree on the type and
// collapse to a single input.
func ScanParams(sql string) ([]Param, error) {
	var params []Param
	seen := make(map[string]Type)
	err := scanSQL(sql, nil, func(p Param) error {
		if prev, ok := seen[p.Name]; ok {
			if prev.String() != p.Type.String() {
				return fmt.Errorf("query parameter %q is declared as both %s and %s; "+
					"every occurrence must use the same type", p.Name, prev, p.Type)
			}
			return nil
		}
		seen[p.Name] = p.Type
		params = append(params, p)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return params, nil
}

// RewriteParams turns each {name:Type} into cast(@name AS Type).
//
// It exists because ClickHouse's server-side parameters travel as text, and
// clickhouse-go does not render every Go value into text the server will
// accept: a time.Time arrives as a Unix timestamp, and a uuid.UUID or a
// decimal.Decimal arrives quoted, all of which the server rejects. The @name
// form uses the driver's client-side binding instead, which renders every type
// correctly, and the cast keeps the declared type explicit.
//
// The rewrite happens at generation time, so the query file itself stays a
// query you can paste into clickhouse-client.
func RewriteParams(sql string) (string, error) {
	sb := &strings.Builder{}
	sb.Grow(len(sql))
	err := scanSQL(sql,
		func(text string) { sb.WriteString(text) },
		func(p Param) error {
			sb.WriteString("cast(@")
			sb.WriteString(p.Name)
			sb.WriteString(" AS ")
			sb.WriteString(p.Type.String())
			sb.WriteString(")")
			return nil
		})
	if err != nil {
		return "", err
	}
	return sb.String(), nil
}
