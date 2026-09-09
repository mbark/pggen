package chtest

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestSplitStatements matters because ClickHouse runs one statement per call,
// so a schema file has to be taken apart before it can be loaded — and a
// semicolon inside a literal or a comment must not split it.
func TestSplitStatements(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want []string
	}{
		{name: "empty", sql: "", want: nil},
		{name: "only_whitespace", sql: "  \n\t ", want: nil},
		{name: "single", sql: "SELECT 1", want: []string{"SELECT 1"}},
		{name: "trailing_semicolon", sql: "SELECT 1;", want: []string{"SELECT 1"}},
		{
			name: "two_statements",
			sql:  "CREATE TABLE a (x String) ENGINE = Memory;\nCREATE TABLE b (y Int64) ENGINE = Memory;",
			want: []string{
				"CREATE TABLE a (x String) ENGINE = Memory",
				"CREATE TABLE b (y Int64) ENGINE = Memory",
			},
		},
		{
			name: "semicolon_in_string_literal",
			sql:  "SELECT 'a;b' AS s; SELECT 2",
			want: []string{"SELECT 'a;b' AS s", "SELECT 2"},
		},
		{
			name: "semicolon_in_line_comment",
			sql:  "SELECT 1 -- trailing ; comment\n; SELECT 2",
			want: []string{"SELECT 1", "SELECT 2"},
		},
		{
			name: "escaped_quote_in_literal",
			sql:  `SELECT 'it\'s; fine' AS s; SELECT 2`,
			want: []string{`SELECT 'it\'s; fine' AS s`, "SELECT 2"},
		},
		{
			name: "enum_definition_keeps_its_commas_and_quotes",
			sql:  "CREATE TABLE t (e Enum8('a;b' = 1, 'c' = 2)) ENGINE = Memory;",
			want: []string{"CREATE TABLE t (e Enum8('a;b' = 1, 'c' = 2)) ENGINE = Memory"},
		},
		{
			name: "empty_statements_are_dropped",
			sql:  "SELECT 1;;\n;SELECT 2;",
			want: []string{"SELECT 1", "SELECT 2"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, SplitStatements(tt.sql)); diff != "" {
				t.Errorf("SplitStatements mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
