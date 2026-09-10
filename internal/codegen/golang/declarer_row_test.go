package golang

import (
	"testing"

	"github.com/mbark/pggen/internal/codegen"
	"github.com/mbark/pggen/internal/codegen/golang/gotype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSharedRowDeclarer_Declare(t *testing.T) {
	cols := []TemplatedColumn{
		{PgName: "id", UpperName: "ID", Type: gotype.Int32, QualType: "int32"},
		{PgName: "name", UpperName: "Name", Type: &gotype.PointerType{Elem: gotype.String}, QualType: "*string"},
	}

	tests := []struct {
		name    string
		dialect codegen.Dialect
		want    string
	}{
		{
			name:    "postgres",
			dialect: codegen.DialectPostgres,
			want: "type ItemRow struct {\n" +
				"\tID   int32   `json:\"id\"`\n" +
				"\tName *string `json:\"name\"`\n" +
				"}",
		},
		{
			// clickhouse-go binds columns to fields by ch tag, so a shared
			// output= struct without them cannot be scanned into.
			name:    "clickhouse carries ch tags",
			dialect: codegen.DialectClickHouse,
			want: "type ItemRow struct {\n" +
				"\tID   int32   `ch:\"id\"   json:\"id\"`\n" +
				"\tName *string `ch:\"name\" json:\"name\"`\n" +
				"}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decl := NewSharedRowDeclarer("ItemRow", cols, tt.dialect)
			assert.Equal(t, "row_struct::ItemRow", decl.DedupeKey())

			got, err := decl.Declare("")
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSharedRowDeclarer_Dedup(t *testing.T) {
	d1 := NewSharedRowDeclarer("ItemRow", nil, codegen.DialectPostgres)
	d2 := NewSharedRowDeclarer("ItemRow", nil, codegen.DialectPostgres)
	d3 := NewSharedRowDeclarer("OtherRow", nil, codegen.DialectPostgres)

	ds := NewDeclarerSet(d1, d2, d3)
	assert.Len(t, ds.ListAll(), 2)
}
