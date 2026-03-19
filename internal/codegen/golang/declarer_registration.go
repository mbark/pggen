package golang

import (
	"github.com/mbark/pggen/internal/codegen/golang/gotype"
	"sort"
	"strconv"
	"strings"
)

// TypeRegistrationDeclarer declares a RegisterTypes function that registers
// composite and enum types with a pgx v5 connection's TypeMap.
type TypeRegistrationDeclarer struct {
	pgTypeNames []string
}

func NewTypeRegistrationDeclarer(names []string) TypeRegistrationDeclarer {
	sorted := make([]string, len(names))
	copy(sorted, names)
	sort.Strings(sorted)
	return TypeRegistrationDeclarer{pgTypeNames: sorted}
}

func (t TypeRegistrationDeclarer) DedupeKey() string {
	return "type_registration"
}

func (t TypeRegistrationDeclarer) Declare(string) (string, error) {
	sb := &strings.Builder{}
	sb.WriteString("// RegisterTypes registers custom Postgres types (composites and enums) with\n")
	sb.WriteString("// the pgx connection's TypeMap so that they can be scanned and encoded\n")
	sb.WriteString("// correctly. Call this once per connection after connecting.\n")
	sb.WriteString("//\n")
	sb.WriteString("// For pgxpool.Pool, use config.AfterConnect:\n")
	sb.WriteString("//\n")
	sb.WriteString("//\tconfig.AfterConnect = func(ctx context.Context, conn *pgx.Conn) error {\n")
	sb.WriteString("//\t\treturn RegisterTypes(ctx, conn)\n")
	sb.WriteString("//\t}\n")
	sb.WriteString("func RegisterTypes(ctx context.Context, conn *pgx.Conn) error {\n")
	sb.WriteString("\t_, err := conn.LoadTypes(ctx, []string{\n")
	for _, name := range t.pgTypeNames {
		sb.WriteString("\t\t")
		sb.WriteString(strconv.Quote(name))
		sb.WriteString(",\n")
	}
	sb.WriteString("\t})\n")
	sb.WriteString("\treturn err\n")
	sb.WriteString("}")
	return sb.String(), nil
}

// collectPgTypeNames walks a gotype.Type tree and collects Postgres type names
// for composite and enum types that need registration.
func collectPgTypeNames(typ gotype.Type, names map[string]struct{}) {
	switch typ := gotype.UnwrapNestedType(typ).(type) {
	case *gotype.CompositeType:
		if typ.PgComposite.Name != "" {
			names[typ.PgComposite.Name] = struct{}{}
		}
		for _, fieldType := range typ.FieldTypes {
			collectPgTypeNames(fieldType, names)
		}
	case *gotype.EnumType:
		if typ.PgEnum.Name != "" {
			names[typ.PgEnum.Name] = struct{}{}
		}
	case *gotype.ArrayType:
		if typ.PgArray.Name != "" {
			names[typ.PgArray.Name] = struct{}{}
		}
		collectPgTypeNames(typ.Elem, names)
	}
}
