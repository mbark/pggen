package chinfer

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/mbark/pggen/internal/ch"
)

func TestScanParams(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []Param
	}{
		{name: "none", sql: "SELECT 1", want: nil},
		{
			name: "single",
			sql:  "SELECT * FROM t WHERE a = {id:UInt64}",
			want: []Param{{Name: "id", Type: ch.Scalar{Name: "UInt64"}}},
		},
		{
			// Modelled on usageservice/sql/get_data_usage_per_msisdn_parseup.sql.
			name: "several_in_order",
			sql: `SELECT a_num, sum(units) FROM parseup.cdr FINAL
			      WHERE a_num IN {msisdns:Array(String)}
			        AND start_date >= {from:DateTime}
			        AND start_date < {to:DateTime}`,
			want: []Param{
				{Name: "msisdns", Type: ch.Array{Elem: ch.Scalar{Name: "String"}}},
				{Name: "from", Type: ch.DateTime{}},
				{Name: "to", Type: ch.DateTime{}},
			},
		},
		{
			// cdrsql/import_source_raw_tele2.sql uses path_prefix twice.
			name: "repeat_collapses_to_one_input",
			sql: `SELECT concat({path_prefix:String}, x) FROM t
			      WHERE concat({path_prefix:String}, y) NOT IN (SELECT p FROM u)`,
			want: []Param{{Name: "path_prefix", Type: ch.Scalar{Name: "String"}}},
		},
		{
			name: "order_is_first_appearance_not_alphabetical",
			sql:  "SELECT {z:String}, {a:String}, {z:String}",
			want: []Param{
				{Name: "z", Type: ch.Scalar{Name: "String"}},
				{Name: "a", Type: ch.Scalar{Name: "String"}},
			},
		},
		{
			name: "whitespace_is_tolerated",
			sql:  "SELECT { spaced : Nullable(String) }",
			want: []Param{{Name: "spaced", Type: ch.Nullable{Elem: ch.Scalar{Name: "String"}}}},
		},
		{
			name: "parameterized_types",
			sql:  "SELECT {d:Decimal(18, 6)}, {ts:DateTime64(3, 'UTC')}, {m:Map(String, String)}",
			want: []Param{
				{Name: "d", Type: ch.Decimal{Precision: 18, Scale: 6}},
				{Name: "ts", Type: ch.DateTime64{Precision: 3, TZ: "UTC"}},
				{Name: "m", Type: ch.Map{KeyType: ch.Scalar{Name: "String"}, ValType: ch.Scalar{Name: "String"}}},
			},
		},

		// Braces that are not parameters.
		{
			name: "brace_in_string_literal_is_not_a_param",
			sql:  "SELECT '{not:AParam}' AS s, {real:UInt8}",
			want: []Param{{Name: "real", Type: ch.Scalar{Name: "UInt8"}}},
		},
		{
			name: "brace_in_line_comment_is_not_a_param",
			sql:  "-- {commented:UInt8}\nSELECT {real:UInt8}",
			want: []Param{{Name: "real", Type: ch.Scalar{Name: "UInt8"}}},
		},
		{
			name: "brace_in_block_comment_is_not_a_param",
			sql:  "/* {commented:UInt8} */ SELECT {real:UInt8}",
			want: []Param{{Name: "real", Type: ch.Scalar{Name: "UInt8"}}},
		},
		{
			name: "brace_in_backtick_identifier_is_not_a_param",
			sql:  "SELECT `{weird:Name}` FROM t WHERE a = {real:UInt8}",
			want: []Param{{Name: "real", Type: ch.Scalar{Name: "UInt8"}}},
		},
		{
			name: "escaped_quote_does_not_end_the_literal",
			sql:  `SELECT 'it\'s {fake:UInt8}' AS s, {real:UInt8}`,
			want: []Param{{Name: "real", Type: ch.Scalar{Name: "UInt8"}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ScanParams(tt.sql)
			if err != nil {
				t.Fatalf("ScanParams returned error: %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("ScanParams mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestScanParams_errors(t *testing.T) {
	tests := []struct{ name, sql, wantSub string }{
		{
			name:    "missing_type",
			sql:     "SELECT {id}",
			wantSub: "missing its type",
		},
		{
			name:    "missing_name",
			sql:     "SELECT {:UInt64}",
			wantSub: "missing its name",
		},
		{
			name:    "unclosed_brace",
			sql:     "SELECT {id:UInt64",
			wantSub: "unclosed {",
		},
		{
			name:    "bad_type",
			sql:     "SELECT {id:Nullable(}",
			wantSub: "id",
		},
		{
			// A genuine bug the scanner can catch for free.
			name:    "same_name_conflicting_types",
			sql:     "SELECT {x:UInt64} FROM t WHERE y = {x:String}",
			wantSub: "declared as both",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ScanParams(tt.sql)
			if err == nil {
				t.Fatalf("ScanParams(%q) succeeded; want an error", tt.sql)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error = %q; want it to contain %q", err, tt.wantSub)
			}
		})
	}
}
