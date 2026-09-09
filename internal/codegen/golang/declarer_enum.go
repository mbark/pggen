package golang

import (
	"github.com/mbark/pggen/internal/codegen/golang/gotype"
	"strconv"
	"strings"
)

// EnumTypeDeclarer declares a new string type and the const values to map to a
// Postgres enum.
type EnumTypeDeclarer struct {
	enum *gotype.EnumType
}

func NewEnumTypeDeclarer(enum *gotype.EnumType) EnumTypeDeclarer {
	return EnumTypeDeclarer{enum: enum}
}

func (e EnumTypeDeclarer) DedupeKey() string {
	return "enum_type::" + e.enum.Name
}

func (e EnumTypeDeclarer) Declare(string) (string, error) {
	sb := &strings.Builder{}
	// Doc string.
	if e.enum.SQLName != "" {
		sb.WriteString("// ")
		sb.WriteString(e.enum.Name)
		sb.WriteString(" represents the ")
		sb.WriteString(e.enum.SQLKindName)
		sb.WriteString(" enum ")
		sb.WriteString(strconv.Quote(e.enum.SQLName))
		sb.WriteString(".\n")
	}
	// Type declaration.
	sb.WriteString("type ")
	sb.WriteString(e.enum.Name)
	sb.WriteString(" string\n\n")
	// Const enum values.
	sb.WriteString("const (\n")
	nameLen := 0
	for _, label := range e.enum.Labels {
		if len(label) > nameLen {
			nameLen = len(label)
		}
	}
	for i, label := range e.enum.Labels {
		sb.WriteString("\t")
		sb.WriteString(label)
		sb.WriteString(strings.Repeat(" ", nameLen+1-len(label)))
		sb.WriteString(e.enum.Name)
		sb.WriteString(` = `)
		sb.WriteString(strconv.Quote(e.enum.Values[i]))
		sb.WriteByte('\n')
	}
	sb.WriteString(")\n\n")
	// Stringer
	dispatcher := strings.ToLower(e.enum.Name)[0]
	sb.WriteString("func (")
	sb.WriteByte(dispatcher)
	sb.WriteByte(' ')
	sb.WriteString(e.enum.Name)
	sb.WriteString(") String() string { return string(")
	sb.WriteByte(dispatcher)
	sb.WriteString(") }")

	if e.enum.SQLKindName == "ClickHouse" {
		// clickhouse-go refuses to scan an Enum column into a named string
		// type, but it honours sql.Scanner, so give the type one. Value keeps
		// the type usable as a query parameter too.
		name := e.enum.Name
		d := string(dispatcher)
		sb.WriteString("\n\n// Scan implements sql.Scanner. clickhouse-go will not convert an enum\n")
		sb.WriteString("// column into a bare named string type, but it honours this.\n")
		sb.WriteString("func (" + d + " *" + name + ") Scan(src any) error {\n")
		sb.WriteString("\tswitch v := src.(type) {\n")
		sb.WriteString("\tcase string:\n")
		sb.WriteString("\t\t*" + d + " = " + name + "(v)\n")
		sb.WriteString("\tcase *string:\n")
		sb.WriteString("\t\tif v != nil {\n")
		sb.WriteString("\t\t\t*" + d + " = " + name + "(*v)\n")
		sb.WriteString("\t\t}\n")
		sb.WriteString("\tdefault:\n")
		sb.WriteString("\t\treturn fmt.Errorf(\"cannot scan %T into " + name + "\", src)\n")
		sb.WriteString("\t}\n")
		sb.WriteString("\treturn nil\n")
		sb.WriteString("}")
	}
	return sb.String(), nil
}
