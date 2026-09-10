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

// FindDeclarers finds all the Declarers needed by typ and the types nested
// inside it. Returns an empty set if no declarations are needed.
//
// Input and output types ask the same question: pgx has to be told about a
// composite or an enum whichever direction it travels in.
func FindDeclarers(typ gotype.Type) DeclarerSet {
	decls := NewDeclarerSet()
	gotype.Walk(typ, func(typ gotype.Type) bool {
		switch typ := typ.(type) {
		case *gotype.CompositeType:
			decls.AddAll(NewCompositeTypeDeclarer(typ))
		case *gotype.EnumType:
			decls.AddAll(NewEnumTypeDeclarer(typ))
		case *gotype.ArrayType:
			// pgx already knows how to translate an array of a builtin, so
			// nothing under it needs declaring either.
			return !gotype.IsPgxSupportedArray(typ)
		}
		return true
	})
	return decls
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
