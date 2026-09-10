package parser

import (
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/mbark/pggen/internal/ast"
	gotok "go/token"
	"strings"
	"testing"
)

func ignoreCommentPos() cmp.Option {
	return cmpopts.IgnoreFields(ast.LineComment{}, "Start")
}

func ignoreQueryPos() cmp.Option {
	return cmpopts.IgnoreFields(ast.SourceQuery{}, "Start", "Semi")
}

func TestParseFile_Queries(t *testing.T) {
	tests := []struct {
		src  string
		want ast.Query
	}{
		{
			"-- name: Qux :many\nSELECT 1;",
			&ast.SourceQuery{
				Name:        "Qux",
				Doc:         &ast.CommentGroup{List: []*ast.LineComment{{Text: "-- name: Qux :many"}}},
				SourceSQL:   "SELECT 1;",
				PreparedSQL: "SELECT 1;",
				ResultKind:  ast.ResultKindMany,
			},
		},
		{
			"-- name: Foo :one\nSELECT 1;",
			&ast.SourceQuery{
				Name:        "Foo",
				Doc:         &ast.CommentGroup{List: []*ast.LineComment{{Text: "-- name: Foo :one"}}},
				SourceSQL:   "SELECT 1;",
				PreparedSQL: "SELECT 1;",
				ResultKind:  ast.ResultKindOne,
			},
		},
		{
			"-- name: Qux   :exec\nSELECT pggen.arg('Bar');",
			&ast.SourceQuery{
				Name:        "Qux",
				Doc:         &ast.CommentGroup{List: []*ast.LineComment{{Text: "-- name: Qux   :exec"}}},
				SourceSQL:   "SELECT pggen.arg('Bar');",
				PreparedSQL: "SELECT $1;",
				ParamNames:  []string{"Bar"},
				ResultKind:  ast.ResultKindExec,
			},
		},
		{
			"-- name: Qux   :exec\nSELECT pggen.arg ('Bar');",
			&ast.SourceQuery{
				Name:        "Qux",
				Doc:         &ast.CommentGroup{List: []*ast.LineComment{{Text: "-- name: Qux   :exec"}}},
				SourceSQL:   "SELECT pggen.arg ('Bar');",
				PreparedSQL: "SELECT $1;",
				ParamNames:  []string{"Bar"},
				ResultKind:  ast.ResultKindExec,
			},
		},
		{
			"-- name: Qux :one\nSELECT pggen.arg('A$_$$B123');",
			&ast.SourceQuery{
				Name:        "Qux",
				Doc:         &ast.CommentGroup{List: []*ast.LineComment{{Text: "-- name: Qux :one"}}},
				SourceSQL:   "SELECT pggen.arg('A$_$$B123');",
				PreparedSQL: "SELECT $1;",
				ParamNames:  []string{"A$_$$B123"},
				ResultKind:  ast.ResultKindOne,
			},
		},
		{
			"-- name: Qux :many\nSELECT pggen.arg('Bar'), pggen.arg('Qux'), pggen.arg('Bar');",
			&ast.SourceQuery{
				Name:        "Qux",
				Doc:         &ast.CommentGroup{List: []*ast.LineComment{{Text: "-- name: Qux :many"}}},
				SourceSQL:   "SELECT pggen.arg('Bar'), pggen.arg('Qux'), pggen.arg('Bar');",
				PreparedSQL: "SELECT $1, $2, $1;",
				ParamNames:  []string{"Bar", "Qux"},
				ResultKind:  ast.ResultKindMany,
			},
		},
		{
			"-- name: Qux :many\nSELECT /*pggen.arg('Bar'),*/ pggen.arg('Qux'), pggen.arg('Bar');",
			&ast.SourceQuery{
				Name:        "Qux",
				Doc:         &ast.CommentGroup{List: []*ast.LineComment{{Text: "-- name: Qux :many"}}},
				SourceSQL:   "SELECT /*pggen.arg('Bar'),*/ pggen.arg('Qux'), pggen.arg('Bar');",
				PreparedSQL: "SELECT /*pggen.arg('Bar'),*/ $1, $2;",
				ParamNames:  []string{"Qux", "Bar"},
				ResultKind:  ast.ResultKindMany,
			},
		},
		{
			"-- name: Qux :many proto-type=foo.Bar\nSELECT 1;",
			&ast.SourceQuery{
				Name:        "Qux",
				Doc:         &ast.CommentGroup{List: []*ast.LineComment{{Text: "-- name: Qux :many proto-type=foo.Bar"}}},
				SourceSQL:   "SELECT 1;",
				PreparedSQL: "SELECT 1;",
				ParamNames:  nil,
				ResultKind:  ast.ResultKindMany,
				Pragmas:     ast.Pragmas{ProtobufType: "foo.Bar"},
			},
		},
		{
			"-- name: Qux :many proto-type=Bar\nSELECT 1;",
			&ast.SourceQuery{
				Name:        "Qux",
				Doc:         &ast.CommentGroup{List: []*ast.LineComment{{Text: "-- name: Qux :many proto-type=Bar"}}},
				SourceSQL:   "SELECT 1;",
				PreparedSQL: "SELECT 1;",
				ParamNames:  nil,
				ResultKind:  ast.ResultKindMany,
				Pragmas:     ast.Pragmas{ProtobufType: "Bar"},
			},
		},
		{
			"-- name: FindItems :many output=ItemRow\nSELECT 1;",
			&ast.SourceQuery{
				Name:        "FindItems",
				Doc:         &ast.CommentGroup{List: []*ast.LineComment{{Text: "-- name: FindItems :many output=ItemRow"}}},
				SourceSQL:   "SELECT 1;",
				PreparedSQL: "SELECT 1;",
				ParamNames:  nil,
				ResultKind:  ast.ResultKindMany,
				Pragmas:     ast.Pragmas{OutputType: "ItemRow"},
			},
		},
		{
			"-- name: GetItem :one output=ItemRow\nSELECT 1;",
			&ast.SourceQuery{
				Name:        "GetItem",
				Doc:         &ast.CommentGroup{List: []*ast.LineComment{{Text: "-- name: GetItem :one output=ItemRow"}}},
				SourceSQL:   "SELECT 1;",
				PreparedSQL: "SELECT 1;",
				ParamNames:  nil,
				ResultKind:  ast.ResultKindOne,
				Pragmas:     ast.Pragmas{OutputType: "ItemRow"},
			},
		},
		{
			"-- name: FindItems :many sql=FindItemsSQL\nSELECT 1;",
			&ast.SourceQuery{
				Name:        "FindItems",
				Doc:         &ast.CommentGroup{List: []*ast.LineComment{{Text: "-- name: FindItems :many sql=FindItemsSQL"}}},
				SourceSQL:   "SELECT 1;",
				PreparedSQL: "SELECT 1;",
				ParamNames:  nil,
				ResultKind:  ast.ResultKindMany,
				Pragmas:     ast.Pragmas{SQLConst: "FindItemsSQL"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			f, err := ParseFile(gotok.NewFileSet(), "", tt.src, Trace)
			if err != nil {
				t.Fatal(err)
			}

			got := f.Queries[0].(*ast.SourceQuery)

			if diff := cmp.Diff(tt.want, got, ignoreCommentPos(), ignoreQueryPos()); diff != "" {
				t.Errorf("ParseFile() query mismatch (-want +got):\n%s", diff)
			}
		})
	}

}

func TestParseFile_Queries_Fuzz(t *testing.T) {
	tests := []struct {
		src string
	}{
		{"-- name: Qux :many\nSELECT '`\\n' as \" joe!@#$%&*()-+=\";"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			_, err := ParseFile(gotok.NewFileSet(), "", tt.src, Trace)
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestParseFile_Pragmas_Invalid covers the pragmas a query file can get wrong.
// Each has to fail at generation, where the message can say what to write
// instead, rather than in the generated Go.
func TestParseFile_Pragmas_Invalid(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantErr string
	}{
		{
			name:    "sql constant must be exported",
			src:     "-- name: FindItems :many sql=findItemsSQL\nSELECT 1;",
			wantErr: "sql constant must start with an uppercase letter",
		},
		{
			name:    "sql constant must be an identifier",
			src:     "-- name: FindItems :many sql=Find.Items\nSELECT 1;",
			wantErr: "sql constant must only contain",
		},
		{
			// The fan-out makes one statement per sort key, so the constant
			// would have to hold one of them and silently not the others.
			name:    "sql with paginate",
			src:     "-- name: FindItems :many output=ItemRow paginate=items_sort sql=FindItemsSQL\nSELECT 1;",
			wantErr: "cannot be used with paginate=items_sort",
		},
		{
			name:    "unknown pragma",
			src:     "-- name: FindItems :many nope=1\nSELECT 1;",
			wantErr: "unsupported pramga",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseFile(gotok.NewFileSet(), "", tt.src, Trace)
			if err == nil {
				t.Fatalf("ParseFile() succeeded, want error containing %q", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("ParseFile() error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}
