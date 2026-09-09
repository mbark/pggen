package ch

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestParse covers the type vocabulary that DESCRIBE actually produces. The
// canonical cases double as round-trip tests: parsing then printing must give
// back the same string.
func TestParse(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want Type
		// str is the expected String() when it differs from src, which happens
		// for the spellings this package canonicalizes.
		str string
	}{
		{name: "string", src: "String", want: Scalar{Name: "String"}},
		{name: "int64", src: "Int64", want: Scalar{Name: "Int64"}},
		{name: "uint32", src: "UInt32", want: Scalar{Name: "UInt32"}},
		{name: "float64", src: "Float64", want: Scalar{Name: "Float64"}},
		{name: "bool", src: "Bool", want: Scalar{Name: "Bool"}},
		{name: "uuid", src: "UUID", want: Scalar{Name: "UUID"}},
		{name: "date", src: "Date", want: Scalar{Name: "Date"}},
		{name: "date32", src: "Date32", want: Scalar{Name: "Date32"}},
		{name: "ipv6", src: "IPv6", want: Scalar{Name: "IPv6"}},

		{name: "fixed_string", src: "FixedString(16)", want: FixedString{N: 16}},

		{name: "nullable", src: "Nullable(String)", want: Nullable{Elem: Scalar{Name: "String"}}},
		{name: "nullable_uuid", src: "Nullable(UUID)", want: Nullable{Elem: Scalar{Name: "UUID"}}},
		{
			name: "low_cardinality",
			src:  "LowCardinality(String)",
			want: LowCardinality{Elem: Scalar{Name: "String"}},
		},
		{
			// The combination that shows up all over parseup.cdr.
			name: "low_cardinality_nullable",
			src:  "LowCardinality(Nullable(String))",
			want: LowCardinality{Elem: Nullable{Elem: Scalar{Name: "String"}}},
		},

		{name: "array", src: "Array(String)", want: Array{Elem: Scalar{Name: "String"}}},
		{
			name: "array_nested",
			src:  "Array(Array(Nullable(String)))",
			want: Array{Elem: Array{Elem: Nullable{Elem: Scalar{Name: "String"}}}},
		},
		{
			name: "map",
			src:  "Map(String, String)",
			want: Map{KeyType: Scalar{Name: "String"}, ValType: Scalar{Name: "String"}},
		},
		{
			name: "map_array_value",
			src:  "Map(String, Array(UInt64))",
			want: Map{KeyType: Scalar{Name: "String"}, ValType: Array{Elem: Scalar{Name: "UInt64"}}},
		},

		{name: "decimal", src: "Decimal(18, 6)", want: Decimal{Precision: 18, Scale: 6}},
		{name: "decimal_38_3", src: "Decimal(38, 3)", want: Decimal{Precision: 38, Scale: 3}},
		{
			// The fixed-width spellings canonicalize to Decimal(P, S).
			name: "decimal64",
			src:  "Decimal64(4)",
			want: Decimal{Precision: 18, Scale: 4},
			str:  "Decimal(18, 4)",
		},
		{name: "decimal32", src: "Decimal32(2)", want: Decimal{Precision: 9, Scale: 2}, str: "Decimal(9, 2)"},
		{name: "decimal128", src: "Decimal128(5)", want: Decimal{Precision: 38, Scale: 5}, str: "Decimal(38, 5)"},
		{name: "decimal256", src: "Decimal256(1)", want: Decimal{Precision: 76, Scale: 1}, str: "Decimal(76, 1)"},

		{name: "datetime_bare", src: "DateTime", want: DateTime{}},
		{name: "datetime_tz", src: "DateTime('UTC')", want: DateTime{TZ: "UTC"}},
		{
			name: "nullable_datetime_tz",
			src:  "Nullable(DateTime('UTC'))",
			want: Nullable{Elem: DateTime{TZ: "UTC"}},
		},
		{name: "datetime64", src: "DateTime64(3)", want: DateTime64{Precision: 3}},
		{
			// Bare DateTime64 means millisecond precision.
			name: "datetime64_bare",
			src:  "DateTime64",
			want: DateTime64{Precision: 3},
			str:  "DateTime64(3)",
		},
		{name: "datetime64_tz", src: "DateTime64(3, 'UTC')", want: DateTime64{Precision: 3, TZ: "UTC"}},
		{
			name: "datetime_tz_with_slash",
			src:  "DateTime64(9, 'Europe/Stockholm')",
			want: DateTime64{Precision: 9, TZ: "Europe/Stockholm"},
		},

		{
			// parseup.cdr's record_type, exactly as DESCRIBE reports it.
			name: "enum8",
			src:  "Enum8('MOC' = 1, 'SMO' = 2, 'GPRS' = 7)",
			want: Enum{Bits: 8, Labels: []string{"MOC", "SMO", "GPRS"}, Values: []int16{1, 2, 7}},
		},
		{
			name: "enum16",
			src:  "Enum16('a' = 1000, 'b' = 2000)",
			want: Enum{Bits: 16, Labels: []string{"a", "b"}, Values: []int16{1000, 2000}},
		},
		{
			name: "enum8_negative",
			src:  "Enum8('neg' = -1, 'zero' = 0)",
			want: Enum{Bits: 8, Labels: []string{"neg", "zero"}, Values: []int16{-1, 0}},
		},
		{
			name: "enum8_escaped_quote",
			src:  `Enum8('it\'s' = 1)`,
			want: Enum{Bits: 8, Labels: []string{"it's"}, Values: []int16{1}},
		},
		{
			name: "nullable_enum",
			src:  "Nullable(Enum8('a' = 1))",
			want: Nullable{Elem: Enum{Bits: 8, Labels: []string{"a"}, Values: []int16{1}}},
		},

		{
			name: "tuple_named",
			src:  "Tuple(a UInt8, b String)",
			want: Tuple{Names: []string{"a", "b"}, Elems: []Type{Scalar{Name: "UInt8"}, Scalar{Name: "String"}}},
		},
		{
			name: "tuple_positional",
			src:  "Tuple(UInt8, String)",
			want: Tuple{Elems: []Type{Scalar{Name: "UInt8"}, Scalar{Name: "String"}}},
		},
		{
			name: "tuple_named_parameterized",
			src:  "Tuple(a Array(String), b Decimal(18, 6))",
			want: Tuple{
				Names: []string{"a", "b"},
				Elems: []Type{Array{Elem: Scalar{Name: "String"}}, Decimal{Precision: 18, Scale: 6}},
			},
		},

		{
			// Types with no Go mapping still parse, so the error can name them.
			name: "nested_is_unsupported",
			src:  "Nested(x UInt8, y String)",
			want: Unsupported{Raw: "Nested(x UInt8, y String)"},
		},
		{
			name: "aggregate_function_is_unsupported",
			src:  "AggregateFunction(sum, UInt64)",
			want: Unsupported{Raw: "AggregateFunction(sum, UInt64)"},
		},
		{
			name: "unsupported_keeps_nested_parens",
			src:  "SimpleAggregateFunction(max, Nullable(DateTime('UTC')))",
			want: Unsupported{Raw: "SimpleAggregateFunction(max, Nullable(DateTime('UTC')))"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.src)
			if err != nil {
				t.Fatalf("Parse(%q) returned error: %v", tt.src, err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("Parse(%q) mismatch (-want +got):\n%s", tt.src, diff)
			}
			want := tt.str
			if want == "" {
				want = tt.src
			}
			if got.String() != want {
				t.Errorf("Parse(%q).String() = %q; want %q", tt.src, got.String(), want)
			}
			if got.Key() != "ch:"+want {
				t.Errorf("Parse(%q).Key() = %q; want %q", tt.src, got.Key(), "ch:"+want)
			}
		})
	}
}

func TestParse_errors(t *testing.T) {
	tests := []struct {
		name string
		src  string
	}{
		{name: "empty", src: ""},
		{name: "trailing_text", src: "String extra"},
		{name: "unterminated_string", src: "DateTime('UTC"},
		{name: "unbalanced_paren", src: "Nullable(String"},
		{name: "unbalanced_unsupported", src: "Nested(x UInt8"},
		{name: "missing_map_value", src: "Map(String)"},
		{name: "decimal_missing_scale", src: "Decimal(18)"},
		{name: "enum_missing_value", src: "Enum8('a')"},
		{name: "enum_non_numeric_value", src: "Enum8('a' = x)"},
		{name: "enum8_value_out_of_range", src: "Enum8('a' = 999)"},
		{name: "fixed_string_not_a_number", src: "FixedString(abc)"},
		{name: "trailing_backslash", src: `DateTime('UTC\`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.src)
			if err == nil {
				t.Fatalf("Parse(%q) = %v; want an error", tt.src, got)
			}
		})
	}
}

func TestMustParse_panicsOnBadInput(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustParse did not panic on a malformed type")
		}
	}()
	MustParse("Nullable(")
}

func TestIsNullable(t *testing.T) {
	tests := []struct {
		src  string
		want bool
	}{
		{src: "String", want: false},
		{src: "Nullable(String)", want: true},
		// LowCardinality is a storage encoding and must be seen through.
		{src: "LowCardinality(Nullable(String))", want: true},
		{src: "LowCardinality(String)", want: false},
		{src: "Array(Nullable(String))", want: false},
		{src: "Nullable(DateTime('UTC'))", want: true},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			if got := IsNullable(MustParse(tt.src)); got != tt.want {
				t.Errorf("IsNullable(%q) = %v; want %v", tt.src, got, tt.want)
			}
		})
	}
}

func TestPayload(t *testing.T) {
	tests := []struct{ src, want string }{
		{src: "String", want: "String"},
		{src: "Nullable(String)", want: "String"},
		{src: "LowCardinality(String)", want: "String"},
		{src: "LowCardinality(Nullable(String))", want: "String"},
		{src: "Array(Nullable(String))", want: "Array(Nullable(String))"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			if got := Payload(MustParse(tt.src)).String(); got != tt.want {
				t.Errorf("Payload(%q) = %q; want %q", tt.src, got, tt.want)
			}
		})
	}
}

// TestQuote_roundTrips guards the escaping used when printing enum labels and
// timezone names back out.
func TestQuote_roundTrips(t *testing.T) {
	for _, label := range []string{"plain", "it's", `back\slash`, "both'\\"} {
		t.Run(label, func(t *testing.T) {
			src := "Enum8(" + quote(label) + " = 1)"
			got, err := Parse(src)
			if err != nil {
				t.Fatalf("Parse(%q): %v", src, err)
			}
			if diff := cmp.Diff([]string{label}, got.(Enum).Labels); diff != "" {
				t.Errorf("label round trip mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
