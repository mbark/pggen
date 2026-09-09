package golang

import (
	"fmt"

	"github.com/mbark/pggen/internal/casing"
	"github.com/mbark/pggen/internal/ch"
	"github.com/mbark/pggen/internal/codegen/golang/gotype"
	"github.com/mbark/pggen/internal/sqltype"
)

// ChTypeResolver handles the mapping between ClickHouse and Go types.
//
// Where PgTypeResolver looks types up by OID, this one dispatches on the
// structure of the ClickHouse type, because that is what a ClickHouse type
// name is: Nullable and Array and Map say what to do, and only the leaves need
// a lookup table.
type ChTypeResolver struct {
	caser     casing.Caser
	overrides map[string]string
}

func NewChTypeResolver(c casing.Caser, overrides map[string]string) ChTypeResolver {
	return ChTypeResolver{caser: c, overrides: overrides}
}

// Resolve maps a ClickHouse type to a Go type. nullable is ignored: unlike
// Postgres, ClickHouse states nullability in the type itself, so it is already
// part of t.
func (tr ChTypeResolver) Resolve(t sqltype.Type, _ bool, pkgPath string) (gotype.Type, error) {
	cht, ok := t.(ch.Type)
	if !ok {
		return nil, fmt.Errorf("resolve %q: not a ClickHouse type (%T)", t.String(), t)
	}
	return tr.resolve(cht, pkgPath)
}

func (tr ChTypeResolver) resolve(t ch.Type, pkgPath string) (gotype.Type, error) {
	// A user override wins, matched against the type as ClickHouse spells it.
	if goType, ok := tr.overrides[t.String()]; ok {
		return gotype.ParseOpaqueType(goType, t)
	}

	switch t := t.(type) {
	case ch.LowCardinality:
		// An encoding, not a different value. Resolve straight through, but
		// let an override on the inner type still apply.
		return tr.resolve(t.Elem, pkgPath)

	case ch.Nullable:
		elem, err := tr.resolve(t.Elem, pkgPath)
		if err != nil {
			return nil, err
		}
		return &gotype.PointerType{Elem: elem}, nil

	case ch.Array:
		elem, err := tr.resolve(t.Elem, pkgPath)
		if err != nil {
			return nil, fmt.Errorf("resolve element of %s: %w", t, err)
		}
		return &gotype.ArrayType{SQLName: t.String(), Elem: elem}, nil

	case ch.Map:
		key, err := tr.resolve(t.KeyType, pkgPath)
		if err != nil {
			return nil, fmt.Errorf("resolve key of %s: %w", t, err)
		}
		val, err := tr.resolve(t.ValType, pkgPath)
		if err != nil {
			return nil, fmt.Errorf("resolve value of %s: %w", t, err)
		}
		return &gotype.MapType{SQLName: t.String(), Key: key, Val: val}, nil

	case ch.Enum:
		// A ClickHouse enum is a string with a constrained set of values, and
		// that is how it comes back over the wire. There is no name to borrow
		// for a Go type either, since the labels are the type. Use --go-type
		// to map a particular enum to something richer.
		return chString, nil

	case ch.Tuple:
		return nil, fmt.Errorf("no Go type for ClickHouse type %s: "+
			"pggen does not generate structs for tuples yet; "+
			"select the fields individually, or map it with --go-type %q=<goType>", t, t.String())

	case ch.Unsupported:
		return nil, fmt.Errorf("no Go type for ClickHouse type %s: "+
			"map it with --go-type %q=<goType>", t.Raw, t.Raw)
	}

	if typ, ok := findChKnownType(t); ok {
		return typ, nil
	}
	return nil, fmt.Errorf("no Go type found for ClickHouse type %s: "+
		"map it with --go-type %q=<goType>", t, t.String())
}
