package golang

import (
	"github.com/mbark/pggen/internal/codegen/golang/gotype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSharedRowDeclarer_Declare(t *testing.T) {
	decl := NewSharedRowDeclarer("ItemRow", []TemplatedColumn{
		{PgName: "id", UpperName: "ID", Type: gotype.Int32, QualType: "int32"},
		{PgName: "name", UpperName: "Name", Type: &gotype.PointerType{Elem: gotype.String}, QualType: "*string"},
	})

	assert.Equal(t, "row_struct::ItemRow", decl.DedupeKey())

	got, err := decl.Declare("")
	require.NoError(t, err)

	want := "type ItemRow struct {\n" +
		"\tID   int32   `json:\"id\"`\n" +
		"\tName *string `json:\"name\"`\n" +
		"}"
	assert.Equal(t, want, got)
}

func TestSharedRowDeclarer_Dedup(t *testing.T) {
	d1 := NewSharedRowDeclarer("ItemRow", nil)
	d2 := NewSharedRowDeclarer("ItemRow", nil)
	d3 := NewSharedRowDeclarer("OtherRow", nil)

	ds := NewDeclarerSet(d1, d2, d3)
	assert.Len(t, ds.ListAll(), 2)
}
