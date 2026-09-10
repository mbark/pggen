package golang

import (
	"strconv"
	"strings"

	"github.com/mbark/pggen/internal/codegen"
)

// SharedRowDeclarer declares a shared row struct used by multiple queries
// that specify the same output= pragma value.
type SharedRowDeclarer struct {
	name    string
	columns []TemplatedColumn
	dialect codegen.Dialect
}

func NewSharedRowDeclarer(name string, columns []TemplatedColumn, dialect codegen.Dialect) SharedRowDeclarer {
	return SharedRowDeclarer{name: name, columns: columns, dialect: dialect}
}

func (d SharedRowDeclarer) DedupeKey() string {
	return "row_struct::" + d.name
}

func (d SharedRowDeclarer) Declare(pkgPath string) (string, error) {
	sb := &strings.Builder{}
	sb.WriteString("type ")
	sb.WriteString(d.name)
	sb.WriteString(" struct {\n")
	writeRowStructFields(sb, d.columns, d.dialect)
	sb.WriteString("}")
	return sb.String(), nil
}

// writeRowStructFields writes the fields of a row struct.
//
// Every row struct goes through here — the per-query ones and the shared
// output= one — because a ClickHouse row struct is only scannable if it
// carries ch tags, and a shared struct that quietly lost them would fail at
// runtime rather than at generation.
func writeRowStructFields(sb *strings.Builder, cols []TemplatedColumn, dialect codegen.Dialect) {
	maxNameLen, maxTypeLen := getLongestOutput(cols)
	// clickhouse-go's Select and ScanStruct bind columns to fields by ch tag,
	// and want a destination for every column the query returns.
	chTags := dialect == codegen.DialectClickHouse
	maxTagLen := 0
	if chTags {
		for _, out := range cols {
			if n := len(chTag(out.PgName)); n > maxTagLen {
				maxTagLen = n
			}
		}
		maxTagLen++ // 1 space to separate the ch tag from the json tag
	}
	for _, out := range cols {
		sb.WriteString("\t")
		sb.WriteString(out.UpperName)
		sb.WriteString(strings.Repeat(" ", maxNameLen-len(out.UpperName)))
		sb.WriteString(out.QualType)
		sb.WriteString(strings.Repeat(" ", maxTypeLen-len(out.QualType)))
		sb.WriteString("`")
		if chTags {
			tag := chTag(out.PgName)
			sb.WriteString(tag)
			sb.WriteString(strings.Repeat(" ", maxTagLen-len(tag)))
		}
		sb.WriteString("json:")
		sb.WriteString(strconv.Quote(out.PgName))
		sb.WriteString("`")
		sb.WriteRune('\n')
	}
}
