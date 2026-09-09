package ch

import (
	"strings"
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
			name: "semicolon_in_block_comment",
			sql:  "CREATE TABLE t (/* cols; see docs */ a String) ENGINE = Memory;",
			want: []string{"CREATE TABLE t (\n a String) ENGINE = Memory"},
		},
		{
			name: "trailing_comment_is_not_a_statement",
			sql:  "SELECT 1;\n-- nothing follows\n",
			want: []string{"SELECT 1"},
		},
		{
			name: "comment_still_separates_tokens",
			sql:  "SELECT 1--c\nFROM t",
			want: []string{"SELECT 1\n\nFROM t"},
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

// TestSingleStatement matters because inference splices the query into a
// larger statement, where a leftover semicolon or a trailing comment breaks
// the wrapping rather than the query.
func TestSingleStatement(t *testing.T) {
	tests := []struct {
		name, sql, want string
	}{
		{name: "plain", sql: "SELECT 1", want: "SELECT 1"},
		{name: "trailing_semicolon", sql: "SELECT 1;\n", want: "SELECT 1"},
		{
			name: "trailing_line_comment",
			sql:  "SELECT a FROM t -- only a\n",
			want: "SELECT a FROM t",
		},
		{
			// TrimRight cannot reach the semicolon here: the trailing run is
			// the comment, not the terminator.
			name: "semicolon_then_comment",
			sql:  "SELECT 1; -- a note",
			want: "SELECT 1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SingleStatement(tt.sql)
			if err != nil {
				t.Fatalf("SingleStatement(%q) returned error: %s", tt.sql, err)
			}
			if got != tt.want {
				t.Errorf("SingleStatement(%q) = %q; want %q", tt.sql, got, tt.want)
			}
		})
	}
}

func TestSingleStatement_errors(t *testing.T) {
	tests := []struct{ name, sql, wantSub string }{
		{name: "empty", sql: "  \n-- nothing here\n", wantSub: "empty"},
		{name: "two_statements", sql: "SELECT 1; SELECT 2", wantSub: "2 statements"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := SingleStatement(tt.sql)
			if err == nil {
				t.Fatalf("SingleStatement(%q) should have returned an error", tt.sql)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error %q should mention %q", err, tt.wantSub)
			}
		})
	}
}
