package gotype

import (
	"bytes"
	"fmt"
	"github.com/mbark/pggen/internal/casing"
	"github.com/mbark/pggen/internal/sqltype"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Type is a Go type.
type Type interface {
	// Import returns the full package path, like "github.com/mbark/pggen/foo".
	// Empty for builtin types.
	Import() string
	// BaseName returns the unqualified, base name of the type, like "Foo" in:
	//   type Foo int, or "[]*Foo".
	BaseName() string
}

type (
	// ArrayType is a Go slice type.
	ArrayType struct {
		SQLName string // name of the backing database array type, like _int4
		Elem    Type   // element type of the slice, like int for []int
	}

	// MapType is a Go map type. ClickHouse has Map(K, V); Postgres has no
	// equivalent, so this is only produced by the ClickHouse resolver.
	MapType struct {
		SQLName string // name of the backing database map type, like Map(String, String)
		Key     Type
		Val     Type
	}

	// CompositeType is a struct type that represents a Postgres composite type.
	CompositeType struct {
		SQLName        string   // name of the backing database composite type
		SQLColumnNames []string // names of the composite's columns, in order
		Name           string   // Go-style type name in UpperCamelCase
		FieldNames     []string // Go-style child names in UpperCamelCase
		FieldTypes     []Type
	}

	// EnumType is a string type with constant values that maps to the labels of
	// a Postgres enum.
	EnumType struct {
		SQLName string // name of the backing database enum type
		Name    string // name of the unqualified Go type
		// Labels of the Postgres enum formatted as Go identifiers ordered in the
		// same order as in Postgres.
		Labels []string
		// The string constant associated with a label. Labels[i] represents
		// Values[i].
		Values []string
	}

	// ImportType is an imported type.
	ImportType struct {
		PkgPath string // fully qualified package path, like "github.com/mbark/pggen"
		Type    Type   // type to import
	}

	// OpaqueType is a type where only the name is known, as with a user-provided
	// custom type.
	OpaqueType struct {
		Name string // name of the unqualified Go type
	}

	// PointerType is a pointer to another Go type.
	PointerType struct {
		Elem Type // the pointed-to type
	}

	// VoidType is a placeholder type that should never appear in output. We need
	// a placeholder to scan pgx rows, but we ultimately ignore the results in the
	// return values.
	VoidType struct{}
)

func (a *ArrayType) Import() string   { return "" }
func (a *ArrayType) BaseName() string { return "[]" + a.Elem.BaseName() }

func (m *MapType) Import() string { return "" }
func (m *MapType) BaseName() string {
	return "map[" + m.Key.BaseName() + "]" + m.Val.BaseName()
}

func (c *CompositeType) Import() string   { return "" }
func (c *CompositeType) BaseName() string { return c.Name }

func (e *EnumType) Import() string   { return "" }
func (e *EnumType) BaseName() string { return e.Name }

func (e *ImportType) Import() string   { return e.PkgPath }
func (e *ImportType) BaseName() string { return e.Type.BaseName() }

func (o *OpaqueType) Import() string   { return "" }
func (o *OpaqueType) BaseName() string { return o.Name }

func (o *PointerType) Import() string   { return "" }
func (o *PointerType) BaseName() string { return "*" + o.Elem.BaseName() }

func (e *VoidType) Import() string   { return "" }
func (e *VoidType) BaseName() string { return "" }

// QualifyType returns the Go qualified type string for typ, relative to
// otherPkgPath. If aliases is non-nil, it maps full package paths to import
// aliases for resolving name collisions.
func QualifyType(typ Type, otherPkgPath string, aliases ...map[string]string) string {
	// A composite type qualifies each of its parts on its own, recursively:
	// the parts can come from different packages, and a map or an array can
	// appear at any depth. Only a leaf carries an import.
	switch t := typ.(type) {
	case *MapType:
		return "map[" + QualifyType(t.Key, otherPkgPath, aliases...) + "]" +
			QualifyType(t.Val, otherPkgPath, aliases...)
	case *ArrayType:
		return "[]" + QualifyType(t.Elem, otherPkgPath, aliases...)
	case *PointerType:
		return "*" + QualifyType(t.Elem, otherPkgPath, aliases...)
	}

	name := typ.BaseName()
	pkg := typ.Import()
	if pkg == "" || pkg == otherPkgPath {
		return name
	}
	// Check for an import alias first.
	shortPkg := ""
	if len(aliases) > 0 && aliases[0] != nil {
		shortPkg = aliases[0][pkg]
	}
	if shortPkg == "" {
		shortPkg = ExtractShortPackage([]byte(pkg))
	}
	return shortPkg + "." + name
}

func NewArrayType(sqlName string, elemType Type) Type {
	return &ArrayType{
		SQLName: sqlName,
		Elem:    elemType,
	}
}

func NewEnumType(pkgPath, sqlName string, sqlLabels []string, caser casing.Caser) Type {
	name := caser.ToUpperGoIdent(sqlName)
	if name == "" {
		name = ChooseFallbackName(sqlName, "UnnamedEnum")
	}
	labels := make([]string, len(sqlLabels))
	values := make([]string, len(sqlLabels))
	for i, label := range sqlLabels {
		ident := caser.ToUpperGoIdent(label)
		if ident == "" {
			ident = ChooseFallbackName(label, "UnnamedLabel"+strconv.Itoa(i))
		}
		labels[i] = name + ident
		values[i] = sqlLabels[i]
	}
	typ := &EnumType{
		SQLName: sqlName,
		Name:    name,
		Labels:  labels,
		Values:  values,
	}
	if pkgPath != "" {
		return &ImportType{
			PkgPath: pkgPath,
			Type:    typ,
		}
	}
	return typ
}

// ParseOpaqueType creates a Type by parsing a fully qualified Go type like
// "github.com/jschaf/custom.Int4" with the backing database type.
//
//   - []int
//   - []*int
//   - *example.com/foo.Qux
//   - []*example.com/foo.Qux
func ParseOpaqueType(qualType string, sqlType sqltype.Type) (Type, error) {
	bs := []byte(qualType)
	isArr := bs[0] == '['
	if isArr {
		if bs[1] != ']' {
			return nil, fmt.Errorf("malformed custom type %q; must have closing bracket", qualType)
		}
		bs = bs[2:]
	}
	isPtr := bs[0] == '*'
	if isPtr {
		bs = bs[1:]
	}
	idx := lastDotOutsideBrackets(bs)
	name := string(bs[idx+1:])
	// For generic types like Range[github.com/jackc/pgx/v5/pgtype.Int8],
	// shorten the fully qualified type inside brackets to use the short
	// package name, e.g. Range[pgtype.Int8].
	if bracketIdx := strings.IndexByte(name, '['); bracketIdx != -1 {
		inner := name[bracketIdx+1:]
		inner = strings.TrimSuffix(inner, "]")
		if dotIdx := lastDotOutsideBrackets([]byte(inner)); dotIdx != -1 {
			innerPkg := inner[:dotIdx]
			innerName := inner[dotIdx+1:]
			shortPkg := ExtractShortPackage([]byte(innerPkg))
			name = name[:bracketIdx+1] + shortPkg + "." + innerName + "]"
		}
	}
	var typ Type = &OpaqueType{Name: name}

	if isQualifiedType := idx != -1; isQualifiedType {
		pkgPath := bs[:idx]
		typ = &ImportType{
			PkgPath: string(pkgPath),
			Type:    typ,
		}
	}

	if isPtr {
		typ = &PointerType{Elem: typ}
	}

	if isArr {
		arr, ok := sqlType.(sqltype.ArrayType)
		// Ensure that if we have a Go slice type that the database type is also
		// an array. []byte is special since it maps to scalar types like the
		// Postgres bytea type.
		if !ok && sqlType != nil && qualType != "[]byte" {
			return nil, fmt.Errorf("opaque database type %T{%+v} for go type %q is not an array type", sqlType, sqlType, qualType)
		}
		sqlName := ""
		if ok {
			sqlName = arr.String()
		}
		typ = &ArrayType{SQLName: sqlName, Elem: typ}
	}

	return typ, nil
}

// MustParseKnownType creates a gotype.Type by parsing a fully qualified Go type
// that pgx supports natively like "github.com/jackc/pgtype.Int4Array", or most
// builtin types like "string" and []*int16.
func MustParseKnownType(qualType string) Type {
	return mustParseOpaqueType(qualType, nil)
}

// MustParseKnownArrayType creates a gotype.Type for a Go slice backed by a
// database array type, like []int32 for _int4.
//
// The database type is not decoration: the array's name is what RegisterTypes
// registers the type under. Taking a sqltype.ArrayType rather than a
// sqltype.Type makes "this Go slice needs a database array" a compile-time
// requirement instead of a run-time check.
func MustParseKnownArrayType(qualType string, sqlType sqltype.ArrayType) Type {
	return mustParseOpaqueType(qualType, sqlType)
}

// mustParseOpaqueType panics on a malformed type. The known-type tables are
// package-level variables, so there is nowhere to return an error to and no
// input but the literals in this repo.
func mustParseOpaqueType(qualType string, sqlType sqltype.Type) Type {
	typ, err := ParseOpaqueType(qualType, sqlType)
	if err != nil {
		panic(err.Error())
	}
	return typ
}

var majorVersionRegexp = regexp.MustCompile(`^v[0-9]+$`)

// ExtractShortPackage gets the last part of a package path like "generate" in
// "github.com/mbark/pggen/generate".
// lastDotOutsideBrackets returns the index of the last '.' in bs that is not
// inside '[' ... ']' brackets. Returns -1 if no such dot exists. This is
// needed to correctly split generic types like
// "github.com/jackc/pgx/v5/pgtype.Range[github.com/jackc/pgx/v5/pgtype.Int8]"
// into package path and type name.
func lastDotOutsideBrackets(bs []byte) int {
	depth := 0
	last := -1
	for i, b := range bs {
		switch b {
		case '[':
			depth++
		case ']':
			depth--
		case '.':
			if depth == 0 {
				last = i
			}
		}
	}
	return last
}

func ExtractShortPackage(pkgPath []byte) string {
	parts := bytes.Split(pkgPath, []byte{'/'})
	shortPkg := parts[len(parts)-1]
	// Skip major version suffixes to get package name.
	if bytes.HasPrefix(shortPkg, []byte{'v'}) && majorVersionRegexp.Match(shortPkg) {
		shortPkg = parts[len(parts)-2]
	}
	return string(shortPkg)
}

func ChooseFallbackName(pgName string, prefix string) string {
	sb := strings.Builder{}
	sb.WriteString(prefix)
	for _, ch := range pgName {
		if unicode.IsLetter(ch) || ch == '_' || unicode.IsDigit(ch) {
			sb.WriteRune(ch)
		}
	}
	return sb.String()
}

// UnwrapNestedType returns the first type under gotype.ImportType or
// gotype.PointerType.
func UnwrapNestedType(typ Type) Type {
	switch typ := typ.(type) {
	case *ImportType:
		return UnwrapNestedType(typ.Type)
	case *PointerType:
		return UnwrapNestedType(typ.Elem)
	default:
		return typ
	}
}

// IsPgxSupportedArray returns true if pgx can handle the translation from the
// Go array type into the Postgres type.
func IsPgxSupportedArray(typ *ArrayType) bool {
	elem := typ.Elem
	if ptr, ok := elem.(*PointerType); ok {
		elem = ptr.Elem
	}

	var pkgPath string
	if imp, ok := elem.(*ImportType); ok {
		pkgPath = imp.PkgPath
		elem = imp.Type
	}

	base, ok := elem.(*OpaqueType)
	if !ok {
		return false
	}

	name := base.Name
	if pkgPath != "" {
		name = pkgPath + "." + name
	}

	switch name {
	case "string", "byte", "rune",
		"int", "int16", "int32", "int64",
		"uint", "uint16", "uint32", "uint64",
		"float32", "float64",
		"time.Time":
		return true
	default:
		return false
	}
}
