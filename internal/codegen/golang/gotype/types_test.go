package gotype

import (
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestMustParseKnownType(t *testing.T) {
	tests := []struct {
		qualType string
		want     Type
	}{
		{
			qualType: "string",
			want:     &OpaqueType{Name: "string"},
		},
		{
			qualType: "*string",
			want:     &PointerType{Elem: &OpaqueType{Name: "string"}},
		},
		{
			qualType: "[]string",
			want:     &ArrayType{Elem: &OpaqueType{Name: "string"}},
		},
		{
			qualType: "[]*string",
			want:     &ArrayType{Elem: &PointerType{Elem: &OpaqueType{Name: "string"}}},
		},
		{
			qualType: "time.Time",
			want: &ImportType{
				PkgPath: "time",
				Type:    &OpaqueType{Name: "Time"},
			},
		},
		{
			qualType: "[]time.Time",
			want: &ArrayType{
				Elem: &ImportType{PkgPath: "time", Type: &OpaqueType{Name: "Time"}},
			},
		},
		{
			qualType: "[]*time.Time",
			want: &ArrayType{
				Elem: &PointerType{
					Elem: &ImportType{PkgPath: "time", Type: &OpaqueType{Name: "Time"}},
				},
			},
		},
		{
			qualType: "[]util/custom/times.Interval",
			want: &ArrayType{
				Elem: &ImportType{PkgPath: "util/custom/times", Type: &OpaqueType{Name: "Interval"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.qualType, func(t *testing.T) {
			got := MustParseKnownType(tt.qualType)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestQualifyType(t *testing.T) {
	tests := []struct {
		name     string
		typ      Type
		otherPkg string
		want     string
	}{
		{
			name:     "string",
			typ:      &OpaqueType{Name: "string"},
			otherPkg: "example.com/foo",
			want:     "string",
		},
		{
			name:     "[]string",
			typ:      &ArrayType{Elem: &OpaqueType{Name: "string"}},
			otherPkg: "example.com/foo",
			want:     "[]string",
		},
		{
			name:     "[]*string",
			typ:      &ArrayType{Elem: &PointerType{Elem: &OpaqueType{Name: "string"}}},
			otherPkg: "example.com/foo",
			want:     "[]*string",
		},
		{
			name:     "foo.com/qux.Bar - example.com/foo",
			typ:      &ImportType{PkgPath: "foo.com/qux", Type: &OpaqueType{Name: "Bar"}},
			otherPkg: "example.com/foo",
			want:     "qux.Bar",
		},
		{
			name:     "[]foo.com/qux.Bar - example.com/foo",
			typ:      &ArrayType{Elem: &ImportType{PkgPath: "foo.com/qux", Type: &OpaqueType{Name: "Bar"}}},
			otherPkg: "example.com/foo",
			want:     "[]qux.Bar",
		},
		{
			name:     "[]example.com/qux.Bar - example.com/foo",
			typ:      &ArrayType{Elem: &ImportType{PkgPath: "example.com/qux", Type: &OpaqueType{Name: "Bar"}}},
			otherPkg: "example.com/foo",
			want:     "[]qux.Bar",
		},
		{
			name:     "[]example.com/foo.Bar - example.com/foo",
			typ:      &ArrayType{Elem: &ImportType{PkgPath: "example.com/foo", Type: &OpaqueType{Name: "Bar"}}},
			otherPkg: "example.com/foo",
			want:     "[]Bar",
		},
		{
			name: "map[string]time.Time",
			typ: &MapType{
				Key: &OpaqueType{Name: "string"},
				Val: &ImportType{PkgPath: "time", Type: &OpaqueType{Name: "Time"}},
			},
			otherPkg: "example.com/foo",
			want:     "map[string]time.Time",
		},
		{
			// ClickHouse spells this Array(Map(String, DateTime)). A map only
			// used to be qualified at the top level, so reaching one through
			// an array panicked.
			name: "[]map[string]time.Time",
			typ: &ArrayType{Elem: &MapType{
				Key: &OpaqueType{Name: "string"},
				Val: &ImportType{PkgPath: "time", Type: &OpaqueType{Name: "Time"}},
			}},
			otherPkg: "example.com/foo",
			want:     "[]map[string]time.Time",
		},
		{
			name: "map[string][]foo.com/qux.Bar",
			typ: &MapType{
				Key: &OpaqueType{Name: "string"},
				Val: &ArrayType{Elem: &ImportType{PkgPath: "foo.com/qux", Type: &OpaqueType{Name: "Bar"}}},
			},
			otherPkg: "example.com/foo",
			want:     "map[string][]qux.Bar",
		},
		{
			// Array(Array(T)): the inner element used to come out unqualified,
			// because only the outermost [] was peeled.
			name: "[][]foo.com/qux.Bar",
			typ: &ArrayType{Elem: &ArrayType{
				Elem: &ImportType{PkgPath: "foo.com/qux", Type: &OpaqueType{Name: "Bar"}},
			}},
			otherPkg: "example.com/foo",
			want:     "[][]qux.Bar",
		},
		{
			// The element's package matching otherPkg means the same package,
			// so the name loses its qualifier but keeps its [].
			name:     "[]foo.Bar - foo",
			typ:      &ArrayType{Elem: &ImportType{PkgPath: "foo", Type: &OpaqueType{Name: "Bar"}}},
			otherPkg: "foo",
			want:     "[]Bar",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := QualifyType(tt.typ, tt.otherPkg, nil)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestWalk(t *testing.T) {
	// []map[string]*foo.Bar, which reaches a leaf through every wrapper.
	leaf := &ImportType{PkgPath: "foo.com/qux", Type: &OpaqueType{Name: "Bar"}}
	typ := &ArrayType{Elem: &MapType{
		Key: &OpaqueType{Name: "string"},
		Val: &PointerType{Elem: leaf},
	}}

	var got []string
	Walk(typ, func(t Type) bool {
		got = append(got, t.BaseName())
		return true
	})
	assert.Equal(t, []string{
		"[]map[string]*Bar", "map[string]*Bar", "string", "*Bar", "Bar", "Bar",
	}, got, "every wrapper and both halves of the map are visited")

	// Returning false prunes the types under the one that returned it.
	var pruned []string
	Walk(typ, func(t Type) bool {
		pruned = append(pruned, t.BaseName())
		_, isMap := t.(*MapType)
		return !isMap
	})
	assert.Equal(t, []string{"[]map[string]*Bar", "map[string]*Bar"}, pruned)
}

func TestWalk_composite(t *testing.T) {
	typ := &CompositeType{
		Name:       "Order",
		FieldNames: []string{"ID", "Placed"},
		FieldTypes: []Type{
			&OpaqueType{Name: "int32"},
			&ImportType{PkgPath: "time", Type: &OpaqueType{Name: "Time"}},
		},
	}
	var got []string
	Walk(typ, func(t Type) bool {
		got = append(got, t.BaseName())
		return true
	})
	assert.Equal(t, []string{"Order", "int32", "Time", "Time"}, got)
}
