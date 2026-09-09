package golang

import (
	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mbark/pggen/internal/casing"
	"github.com/mbark/pggen/internal/codegen/golang/gotype"
	"github.com/mbark/pggen/internal/difftest"
	"github.com/mbark/pggen/internal/pg"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestTypeResolver_Resolve(t *testing.T) {
	testPkgPath := "github.com/mbark/pggen/internal/codegen/golang/test_resolve"
	caser := casing.NewCaser()
	caser.AddAcronym("ios", "IOS")
	caser.AddAcronym("macos", "MacOS")
	caser.AddAcronym("id", "ID")
	pgDeviceEnum := pg.EnumType{Name: "device_type", Labels: []string{"macos", "ios", "web"}}
	goDeviceEnum := &gotype.EnumType{
		SQLName: pgDeviceEnum.Name,
		Name:    "DeviceType",
		Labels:  []string{"DeviceTypeMacOS", "DeviceTypeIOS", "DeviceTypeWeb"},
		Values:  []string{"macos", "ios", "web"},
	}
	tests := []struct {
		name      string
		overrides map[string]string
		pgType    pg.Type
		nullable  bool
		want      gotype.Type
	}{
		{
			name:   "enum",
			pgType: pgDeviceEnum,
			want:   &gotype.ImportType{PkgPath: testPkgPath, Type: goDeviceEnum},
		},
		{
			name:   "enum array",
			pgType: pg.ArrayType{Name: "_device_type", Elem: pgDeviceEnum},
			want: &gotype.ArrayType{
				SQLName: "_device_type",
				Elem:    &gotype.ImportType{PkgPath: testPkgPath, Type: goDeviceEnum},
			},
		},
		{
			name:   "void",
			pgType: pg.VoidType{},
			want:   &gotype.VoidType{},
		},
		{
			name:      "override",
			overrides: map[string]string{"custom_type": "example.com/custom.QualType"},
			pgType:    pg.BaseType{Name: "custom_type"},
			want: &gotype.ImportType{
				PkgPath: "example.com/custom",
				Type:    &gotype.OpaqueType{Name: "QualType"},
			},
		},
		{
			name:      "override pointer",
			overrides: map[string]string{"custom_type": "*example.com/custom.QualType"},
			pgType:    pg.BaseType{Name: "custom_type"},
			want: &gotype.PointerType{
				Elem: &gotype.ImportType{
					PkgPath: "example.com/custom",
					Type:    &gotype.OpaqueType{Name: "QualType"},
				},
			},
		},
		{
			name:      "override pointer array",
			overrides: map[string]string{"_custom_type": "[]*example.com/custom.QualType"},
			pgType:    pg.ArrayType{Name: "_custom_type", Elem: pg.BaseType{Name: "custom_type"}},
			want: &gotype.ArrayType{
				SQLName: "_custom_type",
				Elem: &gotype.PointerType{
					Elem: &gotype.ImportType{
						PkgPath: "example.com/custom",
						Type:    &gotype.OpaqueType{Name: "QualType"},
					},
				},
			},
		},
		{
			name:     "known nonNullable empty",
			pgType:   pg.BaseType{Name: "point", ID: pgtype.PointOID},
			nullable: false,
			want: &gotype.ImportType{
				PkgPath: "github.com/jackc/pgx/v5/pgtype",
				Type:    &gotype.OpaqueType{Name: "Point"},
			},
		},
		{
			name:     "known nullable",
			pgType:   pg.BaseType{Name: "point", ID: pgtype.PointOID},
			nullable: true,
			want: &gotype.ImportType{
				PkgPath: "github.com/jackc/pgx/v5/pgtype",
				Type:    &gotype.OpaqueType{Name: "Point"},
			},
		},
		{
			name:      "bigint - int8",
			overrides: map[string]string{"bigint": "example.com/custom.QualType"},
			pgType:    pg.BaseType{Name: "int8", ID: pgtype.Int8OID},
			want: &gotype.ImportType{
				PkgPath: "example.com/custom",
				Type:    &gotype.OpaqueType{Name: "QualType"},
			},
		},
		{
			name:      "_bigint - _int8",
			overrides: map[string]string{"_bigint": "[]uint16"},
			pgType:    pg.ArrayType{Name: "_int8", Elem: pg.BaseType{Name: "int8", ID: pgtype.Int8OID}},
			want: &gotype.ArrayType{
				SQLName: "_int8",
				Elem:    &gotype.OpaqueType{Name: "uint16"},
			},
		},
		{
			name:      "_real - _float4 custom type",
			overrides: map[string]string{"_real": "[]example.com/custom.F32"},
			pgType:    pg.ArrayType{ID: pgtype.Float4ArrayOID, Name: "_float4", Elem: pg.BaseType{Name: "_float4", ID: pgtype.Float4OID}},
			want: &gotype.ArrayType{
				SQLName: "_float4",
				Elem: &gotype.ImportType{
					PkgPath: "example.com/custom",
					Type:    &gotype.OpaqueType{Name: "F32"},
				},
			},
		},
		{
			name:     "date array",
			pgType:   pg.ArrayType{ID: pgtype.DateArrayOID, Name: "_date", Elem: pg.BaseType{Name: "date", ID: pgtype.DateOID}},
			nullable: false,
			want: &gotype.ArrayType{
				SQLName: "_date",
				Elem: &gotype.ImportType{
					PkgPath: "github.com/jackc/pgx/v5/pgtype",
					Type:    &gotype.OpaqueType{Name: "Date"},
				},
			},
		},
		{
			name:     "nullable enum",
			pgType:   pgDeviceEnum,
			nullable: true,
			want: &gotype.PointerType{
				Elem: &gotype.ImportType{PkgPath: testPkgPath, Type: goDeviceEnum},
			},
		},
		{
			name: "composite",
			pgType: pg.CompositeType{
				Name:        "qux",
				ColumnNames: []string{"id", "foo"},
				ColumnTypes: []pg.Type{pg.Text, pg.Int8},
			},
			nullable: true,
			want: &gotype.ImportType{
				PkgPath: testPkgPath,
				Type: &gotype.CompositeType{
					SQLName:        "qux",
					SQLColumnNames: []string{"id", "foo"},
					Name:           "Qux",
					FieldNames:     []string{"ID", "Foo"},
					FieldTypes: []gotype.Type{
						&gotype.PointerType{Elem: &gotype.OpaqueType{Name: "string"}},
						&gotype.PointerType{Elem: &gotype.OpaqueType{Name: "int"}},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolver := NewPgTypeResolver(caser, tt.overrides)
			got, err := resolver.Resolve(tt.pgType, tt.nullable, testPkgPath)
			if err != nil {
				t.Fatal(err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestType_QualifyRel(t *testing.T) {
	caser := casing.NewCaser()
	tests := []struct {
		typ          gotype.Type
		otherPkgPath string
		want         string
	}{
		{
			typ: gotype.NewEnumType(
				"example.com/foo",
				"device", []string{"macos"},
				caser,
			),
			otherPkgPath: "example.com/bar",
			want:         "foo.Device",
		},
		{
			typ: gotype.NewEnumType(
				"example.com/bar",
				"device", []string{"macos"},
				caser,
			),
			otherPkgPath: "example.com/bar",
			want:         "Device",
		},
		{
			typ:          gotype.MustParseKnownType("example.com/bar.Baz"),
			otherPkgPath: "example.com/bar",
			want:         "Baz",
		},
		{
			typ:          gotype.MustParseKnownType("string"),
			otherPkgPath: "example.com/bar",
			want:         "string",
		},
		{
			typ:          gotype.MustParseKnownType("string"),
			otherPkgPath: "",
			want:         "string",
		},
	}
	for _, tt := range tests {
		t.Run(tt.typ.Import()+"."+tt.typ.BaseName(), func(t *testing.T) {
			got := gotype.QualifyType(tt.typ, tt.otherPkgPath)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCreateCompositeType(t *testing.T) {
	caser := casing.NewCaser()
	resolver := NewPgTypeResolver(caser, nil)
	tests := []struct {
		pkgPath string
		pgType  pg.CompositeType
		want    gotype.Type
	}{
		{
			pkgPath: "example.com/foo",
			pgType: pg.CompositeType{
				Name:        "qux",
				ColumnNames: []string{"one", "two_a"},
				ColumnTypes: []pg.Type{pg.Text, pg.Int8},
			},
			want: &gotype.ImportType{
				PkgPath: "example.com/foo",
				Type: &gotype.CompositeType{
					SQLName:        "qux",
					SQLColumnNames: []string{"one", "two_a"},
					Name:           "Qux",
					FieldNames:     []string{"One", "TwoA"},
					FieldTypes: []gotype.Type{
						&gotype.PointerType{Elem: &gotype.OpaqueType{Name: "string"}},
						&gotype.PointerType{Elem: &gotype.OpaqueType{Name: "int"}},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.pkgPath+" "+tt.pgType.Name, func(t *testing.T) {
			got, err := CreateCompositeType(tt.pkgPath, tt.pgType, resolver, caser)
			assert.NoError(t, err)
			difftest.AssertSame(t, tt.want, got)
		})
	}
}
