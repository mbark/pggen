package golang

import (
	"testing"

	"github.com/mbark/pggen/internal/codegen/golang/gotype"
	"github.com/stretchr/testify/assert"
)

// TestImportSet_AddType matters because a missed import is a generated file
// that does not compile — and the composite types are where it happens, since
// only a leaf carries an import of its own.
func TestImportSet_AddType(t *testing.T) {
	timeTime := &gotype.ImportType{
		PkgPath: "time",
		Type:    &gotype.OpaqueType{Name: "Time"},
	}
	uuidUUID := &gotype.ImportType{
		PkgPath: "github.com/google/uuid",
		Type:    &gotype.OpaqueType{Name: "UUID"},
	}

	tests := []struct {
		name string
		typ  gotype.Type
		want []string
	}{
		{
			name: "a leaf with no package",
			typ:  &gotype.OpaqueType{Name: "string"},
			want: []string{},
		},
		{
			name: "a leaf with a package",
			typ:  timeTime,
			want: []string{"time"},
		},
		{
			// ClickHouse Nullable(DateTime). A pointer reports no import of
			// its own, so a file whose only use of a package was through one
			// did not import it.
			name: "through a pointer",
			typ:  &gotype.PointerType{Elem: timeTime},
			want: []string{"time"},
		},
		{
			name: "through an array",
			typ:  &gotype.ArrayType{Elem: timeTime},
			want: []string{"time"},
		},
		{
			// A map's own Import is empty on purpose: its key and value can
			// come from different packages.
			name: "both halves of a map",
			typ:  &gotype.MapType{Key: uuidUUID, Val: timeTime},
			want: []string{"github.com/google/uuid", "time"},
		},
		{
			// ClickHouse Array(Map(UUID, DateTime)). The array delegates its
			// Import to the element, and the element has none to give, so
			// nothing was imported and the file did not compile.
			name: "a map inside an array",
			typ:  &gotype.ArrayType{Elem: &gotype.MapType{Key: uuidUUID, Val: timeTime}},
			want: []string{"github.com/google/uuid", "time"},
		},
		{
			name: "a composite's fields",
			typ: &gotype.CompositeType{
				Name:       "Row",
				FieldTypes: []gotype.Type{timeTime, &gotype.ArrayType{Elem: uuidUUID}},
			},
			want: []string{"github.com/google/uuid", "time"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := NewImportSet()
			set.AddType(tt.typ)
			assert.Equal(t, tt.want, set.SortedPackages())
		})
	}
}
