package golang

import (
	"strconv"
	"strings"
)

// SharedRowDeclarer declares a shared row struct used by multiple queries
// that specify the same output= pragma value.
type SharedRowDeclarer struct {
	name    string
	columns []TemplatedColumn
}

func NewSharedRowDeclarer(name string, columns []TemplatedColumn) SharedRowDeclarer {
	return SharedRowDeclarer{name: name, columns: columns}
}

func (d SharedRowDeclarer) DedupeKey() string {
	return "row_struct::" + d.name
}

func (d SharedRowDeclarer) Declare(pkgPath string) (string, error) {
	sb := &strings.Builder{}
	sb.WriteString("type ")
	sb.WriteString(d.name)
	sb.WriteString(" struct {\n")
	maxNameLen, maxTypeLen := getLongestOutput(d.columns)
	for _, out := range d.columns {
		sb.WriteString("\t")
		sb.WriteString(out.UpperName)
		sb.WriteString(strings.Repeat(" ", maxNameLen-len(out.UpperName)))
		sb.WriteString(out.QualType)
		sb.WriteString(strings.Repeat(" ", maxTypeLen-len(out.QualType)))
		sb.WriteString("`json:")
		sb.WriteString(strconv.Quote(out.PgName))
		sb.WriteString("`")
		sb.WriteRune('\n')
	}
	sb.WriteString("}")
	return sb.String(), nil
}
