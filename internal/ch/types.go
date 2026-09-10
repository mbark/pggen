// Package ch models the ClickHouse type system.
//
// Unlike Postgres, ClickHouse has no type catalog and no OIDs: a type is
// named structurally, and the name carries everything about it. Nullability
// is part of the type — Nullable(String) — rather than something to infer, and
// an enum's labels live in the type itself rather than in a named catalog
// object. So where internal/pg resolves OIDs against pg_catalog, this package
// parses type names into a tree.
package ch

import (
	"strconv"
	"strings"

	"github.com/mbark/pggen/internal/sqltype"
)

// Type is a ClickHouse type. String reports the canonical ClickHouse spelling,
// which is also the type's identity.
type Type interface {
	sqltype.Type // String() is the canonical spelling; Key() namespaces it
	isCHType()
}

type (
	// Scalar is a type with no parameters, like String, Int64, or UUID.
	Scalar struct{ Name string }

	// FixedString is FixedString(N), a fixed-width byte string.
	FixedString struct{ N int }

	// Decimal is Decimal(P, S). The Decimal32/64/128/256(S) spellings parse to
	// this with the precision their width implies.
	Decimal struct{ Precision, Scale int }

	// DateTime is DateTime, optionally carrying a timezone.
	DateTime struct{ TZ string }

	// DateTime64 is DateTime64(P), optionally carrying a timezone.
	DateTime64 struct {
		Precision int
		TZ        string
	}

	// Nullable is Nullable(T). ClickHouse reports this exactly, so pggen never
	// has to guess whether a column can be null.
	Nullable struct{ Elem Type }

	// LowCardinality is LowCardinality(T), a storage encoding that does not
	// change how the value is represented in Go.
	LowCardinality struct{ Elem Type }

	// Array is Array(T).
	Array struct{ Elem Type }

	// Map is Map(K, V).
	Map struct{ KeyType, ValType Type }

	// Enum is Enum8 or Enum16. ClickHouse enums are anonymous: the labels are
	// part of the type rather than a named object, so two columns with the same
	// labels have the same type.
	Enum struct {
		Bits   int      // 8 or 16
		Labels []string // in declaration order
		Values []int16  // Values[i] is the stored value of Labels[i]
	}

	// Tuple is Tuple(...), either named — Tuple(a UInt8, b String) — or
	// positional.
	Tuple struct {
		Names []string // nil for a positional tuple
		Elems []Type
	}

	// Unsupported is a type that parsed but that pggen has no Go mapping for,
	// like Nested or AggregateFunction. Keeping it lets errors name the type
	// instead of failing to lex.
	Unsupported struct{ Raw string }
)

func (Scalar) isCHType()         {}
func (FixedString) isCHType()    {}
func (Decimal) isCHType()        {}
func (DateTime) isCHType()       {}
func (DateTime64) isCHType()     {}
func (Nullable) isCHType()       {}
func (LowCardinality) isCHType() {}
func (Array) isCHType()          {}
func (Map) isCHType()            {}
func (Enum) isCHType()           {}
func (Tuple) isCHType()          {}
func (Unsupported) isCHType()    {}

func (t Scalar) String() string      { return t.Name }
func (t FixedString) String() string { return "FixedString(" + strconv.Itoa(t.N) + ")" }

func (t Decimal) String() string {
	return "Decimal(" + strconv.Itoa(t.Precision) + ", " + strconv.Itoa(t.Scale) + ")"
}

func (t DateTime) String() string {
	if t.TZ == "" {
		return "DateTime"
	}
	return "DateTime(" + quote(t.TZ) + ")"
}

func (t DateTime64) String() string {
	if t.TZ == "" {
		return "DateTime64(" + strconv.Itoa(t.Precision) + ")"
	}
	return "DateTime64(" + strconv.Itoa(t.Precision) + ", " + quote(t.TZ) + ")"
}

func (t Nullable) String() string       { return "Nullable(" + t.Elem.String() + ")" }
func (t LowCardinality) String() string { return "LowCardinality(" + t.Elem.String() + ")" }
func (t Array) String() string          { return "Array(" + t.Elem.String() + ")" }
func (t Map) String() string            { return "Map(" + t.KeyType.String() + ", " + t.ValType.String() + ")" }

func (t Enum) String() string {
	sb := &strings.Builder{}
	sb.WriteString("Enum")
	sb.WriteString(strconv.Itoa(t.Bits))
	sb.WriteByte('(')
	for i, label := range t.Labels {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(quote(label))
		sb.WriteString(" = ")
		sb.WriteString(strconv.Itoa(int(t.Values[i])))
	}
	sb.WriteByte(')')
	return sb.String()
}

func (t Tuple) String() string {
	sb := &strings.Builder{}
	sb.WriteString("Tuple(")
	for i, elem := range t.Elems {
		if i > 0 {
			sb.WriteString(", ")
		}
		if i < len(t.Names) && t.Names[i] != "" {
			sb.WriteString(t.Names[i])
			sb.WriteByte(' ')
		}
		sb.WriteString(elem.String())
	}
	sb.WriteByte(')')
	return sb.String()
}

func (t Unsupported) String() string { return t.Raw }

// Key implements sqltype.Type. A ClickHouse type is identified by its
// canonical name, since that name fully describes it.
func (t Scalar) Key() string         { return chKey(t) }
func (t FixedString) Key() string    { return chKey(t) }
func (t Decimal) Key() string        { return chKey(t) }
func (t DateTime) Key() string       { return chKey(t) }
func (t DateTime64) Key() string     { return chKey(t) }
func (t Nullable) Key() string       { return chKey(t) }
func (t LowCardinality) Key() string { return chKey(t) }
func (t Array) Key() string          { return chKey(t) }
func (t Map) Key() string            { return chKey(t) }
func (t Enum) Key() string           { return chKey(t) }
func (t Tuple) Key() string          { return chKey(t) }
func (t Unsupported) Key() string    { return chKey(t) }

// chKey namespaces a ClickHouse type name so it can never collide with another
// dialect's type identity.
func chKey(t Type) string { return "ch:" + t.String() }

// ElemType implements sqltype.ArrayType, which the Go code generator type
// asserts on to check that a --go-type slice override is backed by an array.
// The return type has to be sqltype.Type and not ch.Type: Go matches a method
// set exactly, so narrowing it here would silently fail the assertion.
func (t Array) ElemType() sqltype.Type { return t.Elem }

// A missing or mistyped ElemType only shows up as a --go-type override being
// rejected at run time, so pin the interface at compile time instead.
var _ sqltype.ArrayType = Array{}

// IsNullable reports whether values of t can be null, looking through the
// LowCardinality encoding that does not affect nullability.
func IsNullable(t Type) bool {
	_, ok := Unwrap(t).(Nullable)
	return ok
}

// Unwrap strips the LowCardinality encoding, which says how a value is stored
// rather than what it is. It keeps Nullable, which does change the Go type.
func Unwrap(t Type) Type {
	for {
		lc, ok := t.(LowCardinality)
		if !ok {
			return t
		}
		t = lc.Elem
	}
}

// Payload strips both LowCardinality and Nullable to reach the underlying
// value type.
func Payload(t Type) Type {
	for {
		switch v := t.(type) {
		case LowCardinality:
			t = v.Elem
		case Nullable:
			t = v.Elem
		default:
			return t
		}
	}
}

// quote renders s as a ClickHouse single-quoted string literal.
func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return "'" + r.Replace(s) + "'"
}
