package ch

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
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
			want: []Param{{Name: "id", Type: Scalar{Name: "UInt64"}}},
		},
		{
			// Modelled on usageservice/sql/get_data_usage_per_msisdn_parseup.sql.
			name: "several_in_order",
			sql: `SELECT a_num, sum(units) FROM parseup.cdr FINAL
			      WHERE a_num IN {msisdns:Array(String)}
			        AND start_date >= {from:DateTime}
			        AND start_date < {to:DateTime}`,
			want: []Param{
				{Name: "msisdns", Type: Array{Elem: Scalar{Name: "String"}}},
				{Name: "from", Type: DateTime{}},
				{Name: "to", Type: DateTime{}},
			},
		},
		{
			// cdrsql/import_source_raw_tele2.sql uses path_prefix twice.
			name: "repeat_collapses_to_one_input",
			sql: `SELECT concat({path_prefix:String}, x) FROM t
			      WHERE concat({path_prefix:String}, y) NOT IN (SELECT p FROM u)`,
			want: []Param{{Name: "path_prefix", Type: Scalar{Name: "String"}}},
		},
		{
			name: "order_is_first_appearance_not_alphabetical",
			sql:  "SELECT {z:String}, {a:String}, {z:String}",
			want: []Param{
				{Name: "z", Type: Scalar{Name: "String"}},
				{Name: "a", Type: Scalar{Name: "String"}},
			},
		},
		{
			name: "whitespace_is_tolerated",
			sql:  "SELECT { spaced : Nullable(String) }",
			want: []Param{{Name: "spaced", Type: Nullable{Elem: Scalar{Name: "String"}}}},
		},
		{
			name: "parameterized_types",
			sql:  "SELECT {d:Decimal(18, 6)}, {ts:DateTime64(3, 'UTC')}, {m:Map(String, String)}",
			want: []Param{
				{Name: "d", Type: Decimal{Precision: 18, Scale: 6}},
				{Name: "ts", Type: DateTime64{Precision: 3, TZ: "UTC"}},
				{Name: "m", Type: Map{KeyType: Scalar{Name: "String"}, ValType: Scalar{Name: "String"}}},
			},
		},

		// Braces that are not parameters.
		{
			name: "brace_in_string_literal_is_not_a_param",
			sql:  "SELECT '{not:AParam}' AS s, {real:UInt8}",
			want: []Param{{Name: "real", Type: Scalar{Name: "UInt8"}}},
		},
		{
			name: "brace_in_line_comment_is_not_a_param",
			sql:  "-- {commented:UInt8}\nSELECT {real:UInt8}",
			want: []Param{{Name: "real", Type: Scalar{Name: "UInt8"}}},
		},
		{
			name: "brace_in_block_comment_is_not_a_param",
			sql:  "/* {commented:UInt8} */ SELECT {real:UInt8}",
			want: []Param{{Name: "real", Type: Scalar{Name: "UInt8"}}},
		},
		{
			name: "brace_in_backtick_identifier_is_not_a_param",
			sql:  "SELECT `{weird:Name}` FROM t WHERE a = {real:UInt8}",
			want: []Param{{Name: "real", Type: Scalar{Name: "UInt8"}}},
		},
		{
			name: "escaped_quote_does_not_end_the_literal",
			sql:  `SELECT 'it\'s {fake:UInt8}' AS s, {real:UInt8}`,
			want: []Param{{Name: "real", Type: Scalar{Name: "UInt8"}}},
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

// TestRewriteParams covers the rewrite that makes typed Go values bindable.
// ClickHouse's server-side parameters travel as text, and the driver renders
// time.Time, uuid.UUID and decimal.Decimal in forms the server rejects; the
// @name form uses client-side binding, which renders all of them correctly.
func TestRewriteParams(t *testing.T) {
	tests := []struct{ name, sql, want string }{
		{
			name: "no params is unchanged",
			sql:  "SELECT 1",
			want: "SELECT 1",
		},
		{
			name: "single param",
			sql:  "SELECT * FROM t WHERE a = {id:UInt64}",
			want: "SELECT * FROM t WHERE a = cast(@id AS UInt64)",
		},
		{
			name: "several params",
			sql:  "SELECT * FROM t WHERE a IN {ids:Array(String)} AND ts >= {from:DateTime}",
			want: "SELECT * FROM t WHERE a IN cast(@ids AS Array(String)) AND ts >= cast(@from AS DateTime)",
		},
		{
			name: "parameterized types keep their arguments",
			sql:  "SELECT {d:Decimal(18, 6)}, {ts:DateTime64(3, 'UTC')}, {m:Map(String, String)}",
			want: "SELECT cast(@d AS Decimal(18, 6)), cast(@ts AS DateTime64(3, 'UTC')), cast(@m AS Map(String, String))",
		},
		{
			name: "a repeated param is rewritten at every occurrence",
			sql:  "SELECT {p:String} WHERE x = {p:String}",
			want: "SELECT cast(@p AS String) WHERE x = cast(@p AS String)",
		},
		{
			name: "whitespace inside the braces is normalized away",
			sql:  "SELECT { spaced : Nullable(String) }",
			want: "SELECT cast(@spaced AS Nullable(String))",
		},
		{
			name: "enum types keep their labels",
			sql:  "SELECT {e:Enum8('MOC' = 1, 'GPRS' = 7)}",
			want: "SELECT cast(@e AS Enum8('MOC' = 1, 'GPRS' = 7))",
		},

		// Braces that are not parameters must survive untouched.
		{
			name: "brace in a string literal is left alone",
			sql:  "SELECT '{not:AParam}' AS s, {real:UInt8}",
			want: "SELECT '{not:AParam}' AS s, cast(@real AS UInt8)",
		},
		{
			name: "brace in a line comment is left alone",
			sql:  "-- {commented:UInt8}\nSELECT {real:UInt8}",
			want: "-- {commented:UInt8}\nSELECT cast(@real AS UInt8)",
		},
		{
			name: "brace in a block comment is left alone",
			sql:  "/* {commented:UInt8} */ SELECT {real:UInt8}",
			want: "/* {commented:UInt8} */ SELECT cast(@real AS UInt8)",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := RewriteParams(tt.sql)
			if err != nil {
				t.Fatalf("RewriteParams returned error: %v", err)
			}
			if got != tt.want {
				t.Errorf("RewriteParams(%q)\n got: %q\nwant: %q", tt.sql, got, tt.want)
			}
		})
	}
}

func TestRewriteParams_errors(t *testing.T) {
	for _, sql := range []string{"SELECT {id}", "SELECT {:UInt64}", "SELECT {id:UInt64"} {
		t.Run(sql, func(t *testing.T) {
			if _, err := RewriteParams(sql); err == nil {
				t.Errorf("RewriteParams(%q) succeeded; want an error", sql)
			}
		})
	}
}

func TestRenameParams(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "renames every occurrence",
			sql:  "SELECT * FROM t WHERE a = {x:String} OR b = {x:String} LIMIT {limit:UInt32}",
			want: "SELECT * FROM t WHERE a = {p0:String} OR b = {p0:String} LIMIT {p1:UInt32}",
		},
		{
			name: "keeps the declared type",
			sql:  "SELECT {a:Array(String)}, {b:Decimal(18, 6)}",
			want: "SELECT {p0:Array(String)}, {p1:Decimal(18, 6)}",
		},
		{
			name: "leaves braces inside literals and comments alone",
			sql:  "SELECT '{x:String}' -- {y:String}\n, {z:String}",
			want: "SELECT '{x:String}' -- {y:String}\n, {p0:String}",
		},
		{
			name: "no params",
			sql:  "SELECT 1",
			want: "SELECT 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Number the parameters in order of first appearance, the way
			// chinfer does.
			params, err := ScanParams(tt.sql)
			if err != nil {
				t.Fatalf("ScanParams: %v", err)
			}
			index := make(map[string]string, len(params))
			for i, p := range params {
				index[p.Name] = fmt.Sprintf("p%d", i)
			}

			got, err := RenameParams(tt.sql, func(name string) string { return index[name] })
			if err != nil {
				t.Fatalf("RenameParams: %v", err)
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("RenameParams mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestRenameParams_reportsBadParam(t *testing.T) {
	_, err := RenameParams("SELECT {x}", func(string) string { return "p0" })
	if err == nil || !strings.Contains(err.Error(), "missing its type") {
		t.Errorf("RenameParams error = %v, want it to mention the missing type", err)
	}
}
