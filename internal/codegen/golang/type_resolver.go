package golang

import (
	"fmt"
	"github.com/mbark/pggen/internal/casing"
	"github.com/mbark/pggen/internal/codegen/golang/gotype"
	"github.com/mbark/pggen/internal/pg"
	"github.com/mbark/pggen/internal/sqltype"
	"strconv"
	"strings"
)

// TypeResolver maps a database type to the Go type that represents it. Each
// dialect has its own implementation because the type systems don't line up:
// Postgres identifies types by OID and resolves them through the catalog,
// while ClickHouse names them structurally, like "Array(Nullable(String))".
type TypeResolver interface {
	Resolve(t sqltype.Type, nullable bool, pkgPath string) (gotype.Type, error)
}

// PgTypeResolver handles the mapping between Postgres and Go types.
type PgTypeResolver struct {
	caser     casing.Caser
	overrides map[string]string
}

func NewPgTypeResolver(c casing.Caser, overrides map[string]string) PgTypeResolver {
	overs := make(map[string]string, len(overrides))
	for k, v := range overrides {
		for _, alias := range listAliases(k) {
			overs[alias] = v
		}
	}
	return PgTypeResolver{caser: c, overrides: overs}
}

// Resolve maps a Postgres type to a Go type.
func (tr PgTypeResolver) Resolve(t sqltype.Type, nullable bool, pkgPath string) (gotype.Type, error) {
	pgt, ok := t.(pg.Type)
	if !ok {
		return nil, fmt.Errorf("resolve %q: not a Postgres type (%T)", t.String(), t)
	}

	// Custom user override.
	if goType, ok := tr.overrides[pgt.String()]; ok {
		opaque, err := gotype.ParseOpaqueType(goType, pgt)
		if err != nil {
			return nil, fmt.Errorf("resolve custom type: %w", err)
		}
		return opaque, nil
	}

	// Known type.
	var typ gotype.Type
	var isKnownType bool
	if nullable {
		typ, isKnownType = gotype.FindKnownTypeNullable(pgt.OID())
	} else {
		typ, isKnownType = gotype.FindKnownTypeNonNullable(pgt.OID())
	}
	if isKnownType {
		switch typ := typ.(type) {
		case *gotype.ArrayType:
			arrTyp, ok := pgt.(pg.ArrayType)
			if !ok {
				// []byte is special: it's a Go slice but maps to scalar Postgres
				// types like bytea, json, and jsonb.
				if typ.BaseName() == "[]byte" {
					return typ, nil
				}
				return nil, fmt.Errorf("resolve known type %q does not have pg array type %q", typ, pgt)
			}
			typ.SQLName = arrTyp.Name
			return typ, nil
		case *gotype.CompositeType:
			comp := pgt.(pg.CompositeType)
			typ.SQLName = comp.Name
			typ.SQLColumnNames = comp.ColumnNames
			return typ, nil
		case *gotype.ImportType:
			ot := typ.Type.(*gotype.OpaqueType)
			ot.SQLType = pgt
			return typ, nil
		case *gotype.EnumType:
			typ.SQLName = pgt.(pg.EnumType).Name
			return typ, nil
		case *gotype.OpaqueType:
			typ.SQLType = pgt
			return typ, nil
		case *gotype.PointerType:
			return typ, nil
		case *gotype.VoidType:
			return &gotype.VoidType{}, nil
		default:
			return nil, fmt.Errorf("resolve unhandled known postgres type %T", typ)
		}
	}

	// New type that pggen will define in generated source code.
	switch pgt := pgt.(type) {
	case pg.ArrayType:
		elemType, err := tr.Resolve(pgt.Elem, nullable, pkgPath)
		if err != nil {
			return nil, fmt.Errorf("resolve array elem type for array type %q: %w", pgt.Name, err)
		}
		return gotype.NewArrayType(pgt.Name, elemType), nil
	case pg.EnumType:
		enum := gotype.NewEnumType(pkgPath, pgt.Name, pgt.Name, "Postgres", pgt.Labels, tr.caser)
		if nullable {
			return &gotype.PointerType{Elem: enum}, nil
		}
		return enum, nil
	case pg.CompositeType:
		comp, err := CreateCompositeType(pkgPath, pgt, tr, tr.caser)
		if err != nil {
			return nil, fmt.Errorf("create composite type: %w", err)
		}
		return comp, nil
	}

	return nil, fmt.Errorf("no go type found for Postgres type %s oid=%d", pgt.String(), pgt.OID())
}

// CreateCompositeType creates a struct to represent a Postgres composite type.
// The type is rooted under pkgPath.
func CreateCompositeType(
	pkgPath string,
	pgt pg.CompositeType,
	resolver PgTypeResolver,
	caser casing.Caser,
) (gotype.Type, error) {
	name := caser.ToUpperGoIdent(pgt.Name)
	if name == "" {
		name = gotype.ChooseFallbackName(pgt.Name, "UnnamedStruct")
	}
	fieldNames := make([]string, len(pgt.ColumnNames))
	fieldTypes := make([]gotype.Type, len(pgt.ColumnTypes))
	for i, colName := range pgt.ColumnNames {
		ident := caser.ToUpperGoIdent(colName)
		if ident == "" {
			ident = gotype.ChooseFallbackName(colName, "UnnamedField"+strconv.Itoa(i))
		}
		fieldNames[i] = ident
		fieldType, err := resolver.Resolve(pgt.ColumnTypes[i] /*nullable*/, true, pkgPath)
		if err != nil {
			return nil, fmt.Errorf("resolve composite column type %s.%s: %w", pgt.Name, colName, err)
		}
		fieldTypes[i] = fieldType
	}
	ct := &gotype.CompositeType{
		SQLName:        pgt.Name,
		SQLColumnNames: pgt.ColumnNames,
		Name:           name,
		FieldNames:     fieldNames,
		FieldTypes:     fieldTypes,
	}
	if pkgPath != "" {
		return &gotype.ImportType{PkgPath: pkgPath, Type: ct}, nil
	}
	return ct, nil
}

func listAliases(name string) []string {
	if strings.HasPrefix(name, "_") {
		aliases := listElemAliases(name[1:])
		for i, alias := range aliases {
			aliases[i] = "_" + alias
		}
		return aliases
	}
	return listElemAliases(name)
}

// listElemAliases lists all known type aliases for a type name. The requested
// type name is included in the list.
// https://www.postgresql.org/docs/13/datatype.html#DATATYPE-TABLE
func listElemAliases(name string) []string {
	switch name {
	case "bigint", "int8":
		return []string{"bigint", "int8"}

	case "bigserial", "serial8":
		return []string{"bigserial", "serial8"}

	case "bool", "boolean":
		return []string{"bool", "boolean"}

	case "float8", "double precision":
		return []string{"float8", "double precision"}

	case "int", "integer", "int4":
		return []string{"int", "integer", "int4"}

	case "real", "float4":
		return []string{"real", "float4"}

	case "smallint", "int2":
		return []string{"smallint", "int2"}

	case "smallserial", "serial2":
		return []string{"smallserial", "serial2"}

	case "serial", "serial4":
		return []string{"serial", "serial4"}

	default:
		// TODO: numeric, multi word aliases
		return []string{name}
	}
}
