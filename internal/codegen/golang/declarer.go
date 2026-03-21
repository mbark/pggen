package golang

import (
	"github.com/mbark/pggen/internal/codegen/golang/gotype"
	"sort"
)

// Declarer is implemented by any value that needs to declare types, data, or
// functions before use. For example, Postgres enums map to a Go enum with a
// type declaration and const values. If we use the enum in any Querier
// function, we need to declare the enum.
type Declarer interface {
	// DedupeKey uniquely identifies the declaration so that we only emit
	// declarations once. Should be namespaced like enum::some_enum.
	DedupeKey() string
	// Declare returns the string of the Go code for the declaration.
	Declare(pkgPath string) (string, error)
}

// DeclarerSet is a set of declarers, identified by the dedupe key.
type DeclarerSet map[string]Declarer

func NewDeclarerSet(decls ...Declarer) DeclarerSet {
	d := DeclarerSet(make(map[string]Declarer, len(decls)))
	d.AddAll(decls...)
	return d
}

func (d DeclarerSet) AddAll(decls ...Declarer) {
	for _, decl := range decls {
		d[decl.DedupeKey()] = decl
	}
}

// ListAll gets all declarers in the set in a stable sort order.
func (d DeclarerSet) ListAll() []Declarer {
	decls := make([]Declarer, 0, len(d))
	for _, decl := range d {
		decls = append(decls, decl)
	}
	sort.Slice(decls, func(i, j int) bool { return decls[i].DedupeKey() < decls[j].DedupeKey() })
	return decls
}

// FindInputDeclarers finds all necessary Declarers for types that appear in
// the input parameters. Returns nil if no declarers are needed.
func FindInputDeclarers(typ gotype.Type) DeclarerSet {
	decls := NewDeclarerSet()
	findInputDeclsHelper(typ, decls)
	return decls
}

func findInputDeclsHelper(typ gotype.Type, decls DeclarerSet) {
	switch typ := gotype.UnwrapNestedType(typ).(type) {
	case *gotype.CompositeType:
		decls.AddAll(
			NewCompositeTypeDeclarer(typ),
		)
		for _, childType := range typ.FieldTypes {
			findInputDeclsHelper(childType, decls)
		}

	case *gotype.ArrayType:
		if gotype.IsPgxSupportedArray(typ) {
			return
		}
		findInputDeclsHelper(typ.Elem, decls)

	case *gotype.EnumType:
		decls.AddAll(
			NewEnumTypeDeclarer(typ),
		)

	default:
		return
	}
}

// FindOutputDeclarers finds all necessary Declarers for types that appear in
// the output rows. Returns nil if no declarers are needed.
func FindOutputDeclarers(typ gotype.Type) DeclarerSet {
	decls := NewDeclarerSet()
	findOutputDeclsHelper(typ, decls)
	return decls
}

func findOutputDeclsHelper(typ gotype.Type, decls DeclarerSet) {
	switch typ := gotype.UnwrapNestedType(typ).(type) {
	case *gotype.EnumType:
		decls.AddAll(
			NewEnumTypeDeclarer(typ),
		)

	case *gotype.CompositeType:
		decls.AddAll(
			NewCompositeTypeDeclarer(typ),
		)
		for _, childType := range typ.FieldTypes {
			findOutputDeclsHelper(childType, decls)
		}

	case *gotype.ArrayType:
		if gotype.IsPgxSupportedArray(typ) {
			return
		}
		findOutputDeclsHelper(typ.Elem, decls)

	default:
		return
	}
}

// ConstantDeclarer declares a new string literal.
type ConstantDeclarer struct {
	key string
	str string
}

func NewConstantDeclarer(key, str string) ConstantDeclarer {
	return ConstantDeclarer{key, str}
}

func (c ConstantDeclarer) DedupeKey() string              { return c.key }
func (c ConstantDeclarer) Declare(string) (string, error) { return c.str, nil }
