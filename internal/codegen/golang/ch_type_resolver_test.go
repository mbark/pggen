package golang

import (
	"testing"

	"github.com/mbark/pggen/internal/casing"
	"github.com/mbark/pggen/internal/ch"
	"github.com/mbark/pggen/internal/codegen/golang/gotype"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resolveCh resolves the ClickHouse type named by s and renders it the way the
// generated code would. Going through ch.Parse rather than building the type
// tree by hand keeps the cases readable and covers the parser at the same
// time.
func resolveCh(t *testing.T, s string, overrides map[string]string) string {
	t.Helper()
	caser := casing.NewCaser()
	typ, err := ch.Parse(s)
	require.NoError(t, err, "parse %q", s)

	got, err := NewChTypeResolver(caser, overrides).Resolve(typ, false, "example.com/gen")
	require.NoError(t, err, "resolve %q", s)
	return gotype.QualifyType(got, "example.com/gen")
}

// TestChTypeResolver_knownTypes pins the leaf type table. Every row is here
// because a wrong Go type or import path in one of them is invisible until a
// query happens to select that type, and the examples only reach a handful.
func TestChTypeResolver_knownTypes(t *testing.T) {
	tests := []struct{ chType, want string }{
		{"String", "string"},
		{"FixedString(16)", "string"},
		{"Bool", "bool"},

		{"Int8", "int8"},
		{"Int16", "int16"},
		{"Int32", "int32"},
		{"Int64", "int64"},
		{"UInt8", "uint8"},
		{"UInt16", "uint16"},
		{"UInt32", "uint32"},
		{"UInt64", "uint64"},

		{"Float32", "float32"},
		{"Float64", "float64"},

		// No Go builtin is wide enough for the 128- and 256-bit integers.
		{"Int128", "*big.Int"},
		{"Int256", "*big.Int"},
		{"UInt128", "*big.Int"},
		{"UInt256", "*big.Int"},

		{"Decimal(18, 6)", "decimal.Decimal"},
		{"Decimal32(4)", "decimal.Decimal"},
		{"Decimal64(4)", "decimal.Decimal"},
		{"Decimal128(4)", "decimal.Decimal"},
		{"Decimal256(4)", "decimal.Decimal"},

		{"Date", "time.Time"},
		{"Date32", "time.Time"},
		{"DateTime", "time.Time"},
		{"DateTime('UTC')", "time.Time"},
		{"DateTime64(3)", "time.Time"},
		{"DateTime64(3, 'UTC')", "time.Time"},

		{"UUID", "uuid.UUID"},
		{"IPv4", "netip.Addr"},
		{"IPv6", "netip.Addr"},
	}
	for _, tt := range tests {
		t.Run(tt.chType, func(t *testing.T) {
			assert.Equal(t, tt.want, resolveCh(t, tt.chType, nil))
		})
	}
}

// TestChTypeResolver_wrappers covers the types the resolver handles
// structurally rather than by table lookup.
func TestChTypeResolver_wrappers(t *testing.T) {
	tests := []struct {
		name, chType, want string
	}{
		{
			name: "Nullable becomes a pointer",
			// This is where ClickHouse is easier than Postgres: nullability is
			// in the type, so it needs no heuristic.
			chType: "Nullable(String)", want: "*string",
		},
		{
			name:   "LowCardinality is transparent",
			chType: "LowCardinality(String)", want: "string",
		},
		{
			name: "LowCardinality(Nullable(T)) is a pointer",
			// The order matters: ClickHouse writes the encoding outside the
			// nullability, and only the nullability reaches Go.
			chType: "LowCardinality(Nullable(String))", want: "*string",
		},
		{
			name:   "Array becomes a slice",
			chType: "Array(String)", want: "[]string",
		},
		{
			name:   "nested Array",
			chType: "Array(Array(Int64))", want: "[][]int64",
		},
		{
			name:   "Array of Nullable",
			chType: "Array(Nullable(UUID))", want: "[]*uuid.UUID",
		},
		{
			name:   "Map becomes a map",
			chType: "Map(String, String)", want: "map[string]string",
		},
		{
			name:   "Map with a non-string value",
			chType: "Map(String, Array(Int64))", want: "map[string][]int64",
		},
		{
			name: "an enum is a string",
			// The labels are the type — there is no name to derive a Go type
			// from, and two columns with the same labels are the same type.
			chType: "Enum8('MOC' = 1, 'GPRS' = 7)", want: "string",
		},
		{
			name:   "Enum16 likewise",
			chType: "Enum16('a' = 1)", want: "string",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolveCh(t, tt.chType, nil))
		})
	}
}

// TestChTypeResolver_overrides covers --go-type, which is the documented
// escape hatch for a type pggen maps too loosely — an enum being the example
// the README gives.
func TestChTypeResolver_overrides(t *testing.T) {
	tests := []struct {
		name, chType, want string
		overrides          map[string]string
	}{
		{
			name:      "override a scalar",
			overrides: map[string]string{"String": "example.com/brand.Name"},
			chType:    "String",
			want:      "brand.Name",
		},
		{
			name: "override an enum, matched on the type as ClickHouse spells it",
			overrides: map[string]string{
				"Enum8('MOC' = 1, 'GPRS' = 7)": "example.com/cdr.RecordType",
			},
			chType: "Enum8('MOC' = 1, 'GPRS' = 7)",
			want:   "cdr.RecordType",
		},
		{
			name:      "override a type pggen has no mapping for at all",
			overrides: map[string]string{"Tuple(a String, b Int64)": "example.com/cdr.Pair"},
			chType:    "Tuple(a String, b Int64)",
			want:      "cdr.Pair",
		},
		{
			name:      "an override applies inside a wrapper",
			overrides: map[string]string{"String": "example.com/brand.Name"},
			chType:    "Array(String)",
			want:      "[]brand.Name",
		},
		{
			name:      "an override on the inner type survives LowCardinality",
			overrides: map[string]string{"String": "example.com/brand.Name"},
			chType:    "LowCardinality(String)",
			want:      "brand.Name",
		},
		{
			name:      "a Nullable override still becomes a pointer",
			overrides: map[string]string{"String": "example.com/brand.Name"},
			chType:    "Nullable(String)",
			want:      "*brand.Name",
		},
		{
			name: "an override on the Nullable type itself wins over the pointer",
			overrides: map[string]string{
				"Nullable(String)": "example.com/brand.MaybeName",
			},
			chType: "Nullable(String)",
			want:   "brand.MaybeName",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, resolveCh(t, tt.chType, tt.overrides))
		})
	}
}

// TestChTypeResolver_errors covers the types pggen refuses. The message is the
// feature here: it has to say what to do next, because the user's only way out
// is --go-type or a different query.
func TestChTypeResolver_errors(t *testing.T) {
	tests := []struct {
		name, chType string
		wantContains []string
	}{
		{
			name:   "a tuple has no Go type",
			chType: "Tuple(a String, b Int64)",
			wantContains: []string{
				"does not generate structs for tuples",
				"select the fields individually",
				"--go-type",
			},
		},
		{
			name:         "an unsupported type names itself",
			chType:       "AggregateFunction(sum, Int64)",
			wantContains: []string{"AggregateFunction(sum, Int64)", "--go-type"},
		},
		{
			name:         "an unknown scalar",
			chType:       "Nothing",
			wantContains: []string{"Nothing", "--go-type"},
		},
		{
			name:         "the error reaches through an Array",
			chType:       "Array(Tuple(a String))",
			wantContains: []string{"resolve element of Array(Tuple(a String))"},
		},
		{
			name:         "the error reaches through a Map value",
			chType:       "Map(String, Tuple(a String))",
			wantContains: []string{"resolve value of Map(String, Tuple(a String))"},
		},
		{
			name:         "the error reaches through a Map key",
			chType:       "Map(Tuple(a String), String)",
			wantContains: []string{"resolve key of Map(Tuple(a String), String)"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			typ, err := ch.Parse(tt.chType)
			require.NoError(t, err)

			_, err = NewChTypeResolver(casing.NewCaser(), nil).Resolve(typ, false, "example.com/gen")
			require.Error(t, err)
			for _, want := range tt.wantContains {
				assert.ErrorContains(t, err, want)
			}
		})
	}
}

// The resolver is reached through the shared TypeResolver interface, so it can
// be handed a Postgres type by a caller that mixed up the dialects. It says so
// rather than panicking.
func TestChTypeResolver_rejectsAForeignType(t *testing.T) {
	_, err := NewChTypeResolver(casing.NewCaser(), nil).Resolve(notAChType{}, false, "")
	assert.ErrorContains(t, err, "not a ClickHouse type")
}

type notAChType struct{}

func (notAChType) String() string { return "int8" }
func (notAChType) Key() string    { return "pg:20" }
