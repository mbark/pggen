package ch

import (
	"fmt"
	"strings"
)

// ZeroLiteral renders a value of t as ClickHouse text.
//
// It exists because of an asymmetry with Postgres. Postgres infers a prepared
// statement's parameter types server-side, so pggen never supplies values to
// learn a query's shape. ClickHouse has no PREPARE: DESCRIBE parses the query
// and fails with "Substitution `x` is not set" unless every parameter has a
// value, even though the values never affect the result columns. So inference
// binds a throwaway literal for each parameter, and this produces it.
//
// Identifier parameters have no such literal — a made-up table name doesn't
// resolve — so they return an error saying so.
func ZeroLiteral(t Type) (string, error) {
	switch t := t.(type) {
	case Nullable:
		// A literal of the element type rather than NULL: NULL is rejected
		// where the query uses the parameter in a position that needs a value.
		return ZeroLiteral(t.Elem)
	case LowCardinality:
		return ZeroLiteral(t.Elem)
	case Array:
		return "[]", nil
	case Map:
		return "{}", nil
	case FixedString:
		return "", nil
	case Decimal:
		return "0", nil
	case DateTime:
		return "1970-01-01 00:00:00", nil
	case DateTime64:
		return "1970-01-01 00:00:00", nil
	case Enum:
		if len(t.Labels) == 0 {
			return "", fmt.Errorf("clickhouse enum type %s has no labels", t)
		}
		// Any declared label parses; the first one always exists.
		return t.Labels[0], nil
	case Tuple:
		parts := make([]string, len(t.Elems))
		for i, elem := range t.Elems {
			lit, err := ZeroLiteral(elem)
			if err != nil {
				return "", err
			}
			parts[i] = lit
		}
		return "(" + strings.Join(parts, ",") + ")", nil
	case Scalar:
		return scalarZero(t.Name)
	case Unsupported:
		return "", fmt.Errorf("cannot infer a query with a %s parameter: "+
			"pggen has no value it can bind for that type", t.Raw)
	}
	return "", fmt.Errorf("unhandled clickhouse type %s", t)
}

func scalarZero(name string) (string, error) {
	switch name {
	case "String":
		return "", nil
	case "Bool", "Boolean":
		return "false", nil
	case "Int8", "Int16", "Int32", "Int64", "Int128", "Int256",
		"UInt8", "UInt16", "UInt32", "UInt64", "UInt128", "UInt256",
		"Float32", "Float64":
		return "0", nil
	case "Date", "Date32":
		return "1970-01-01", nil
	case "DateTime", "DateTime64":
		return "1970-01-01 00:00:00", nil
	case "UUID":
		return "00000000-0000-0000-0000-000000000000", nil
	case "IPv4":
		return "0.0.0.0", nil
	case "IPv6":
		return "::", nil
	case "Identifier":
		return "", fmt.Errorf("cannot infer a query with an {…:Identifier} parameter: " +
			"pggen would have to substitute a real table or column name to run DESCRIBE. " +
			"Interpolate the identifier in Go instead, and keep pggen parameters for values")
	}
	return "", fmt.Errorf("cannot infer a query with a %s parameter: "+
		"pggen has no literal it can bind for that type", name)
}
