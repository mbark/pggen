package golang

import (
	"github.com/mbark/pggen/internal/codegen/golang/gotype"
	"strconv"
	"strings"
)

// CompositeTypeDeclarer declares a new Go struct to represent a Postgres
// composite type.
type CompositeTypeDeclarer struct {
	comp *gotype.CompositeType
}

func NewCompositeTypeDeclarer(comp *gotype.CompositeType) CompositeTypeDeclarer {
	return CompositeTypeDeclarer{comp: comp}
}

func (c CompositeTypeDeclarer) DedupeKey() string {
	return "composite::" + c.comp.Name
}

func (c CompositeTypeDeclarer) Declare(pkgPath string) (string, error) {
	sb := &strings.Builder{}
	// Doc string
	if c.comp.SQLName != "" {
		sb.WriteString("// ")
		sb.WriteString(c.comp.Name)
		sb.WriteString(" represents the Postgres composite type ")
		sb.WriteString(strconv.Quote(c.comp.SQLName))
		sb.WriteString(".\n")
	}
	// Struct declaration.
	sb.WriteString("type ")
	sb.WriteString(c.comp.Name)
	sb.WriteString(" struct")
	if len(c.comp.FieldNames) == 0 {
		sb.WriteString("{") // type Foo struct{}
	} else {
		sb.WriteString(" {\n") // type Foo struct {\n
	}
	// Struct fields. Qualifying a type is not free and the widths need every
	// one of them, so qualify each once and lay the fields out from that.
	qualTypes := make([]string, len(c.comp.FieldTypes))
	for i, fieldType := range c.comp.FieldTypes {
		qualTypes[i] = gotype.QualifyType(fieldType, pkgPath, nil)
	}
	nameLen, typeLen := longestNameType(c.comp.FieldNames, qualTypes)
	for i, name := range c.comp.FieldNames {
		// Name
		sb.WriteRune('\t')
		sb.WriteString(name)
		// Type
		sb.WriteString(strings.Repeat(" ", nameLen-len(name)))
		sb.WriteString(qualTypes[i])
		// JSON struct tag
		sb.WriteString(strings.Repeat(" ", typeLen-len(qualTypes[i])))
		sb.WriteString("`json:")
		sb.WriteString(strconv.Quote(c.comp.SQLColumnNames[i]))
		sb.WriteString("`")
		sb.WriteRune('\n')
	}
	sb.WriteString("}")
	return sb.String(), nil
}

// longestNameType returns the column widths that align a composite's struct
// fields: the longest field name and the longest qualified type, each plus the
// single space that separates it from what follows.
func longestNameType(names, qualTypes []string) (int, int) {
	nameLen := 0
	for _, name := range names {
		nameLen = max(nameLen, len(name))
	}
	typeLen := 0
	for _, qualType := range qualTypes {
		typeLen = max(typeLen, len(qualType))
	}
	return nameLen + 1, typeLen + 1
}
