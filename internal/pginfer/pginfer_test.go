package pginfer

import (
	"context"
	"errors"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/mbark/pggen/internal/ast"
	"github.com/mbark/pggen/internal/codegen"
	"github.com/mbark/pggen/internal/difftest"
	"github.com/mbark/pggen/internal/pg"
	"github.com/mbark/pggen/internal/pgtest"
	"github.com/mbark/pggen/internal/texts"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestInferrer_InferTypes(t *testing.T) {
	conn := pgtest.NewPostgresSchemaString(t, texts.Dedent(`
		CREATE TABLE author (
			author_id  serial PRIMARY KEY,
			first_name text NOT NULL,
			last_name  text NOT NULL,
			suffix text NULL
		);

		CREATE TYPE device_type AS enum (
			'phone',
			'laptop'
		);

		CREATE DOMAIN us_postal_code AS text;
	`))
	q := pg.NewQuerier(conn)
	deviceTypeOID, err := q.FindOIDByName(context.Background(), "device_type")
	require.NoError(t, err)
	deviceTypeArrOID, err := q.FindOIDByName(context.Background(), "_device_type")
	require.NoError(t, err)

	tests := []struct {
		name  string
		query *ast.SourceQuery
		want  codegen.TypedQuery
	}{
		{
			name: "literal query",
			query: &ast.SourceQuery{
				Name:        "LiteralQuery",
				PreparedSQL: "SELECT 1 as one, 'foo' as two",
				ResultKind:  ast.ResultKindOne,
			},
			want: codegen.TypedQuery{
				Name:        "LiteralQuery",
				ResultKind:  ast.ResultKindOne,
				PreparedSQL: "SELECT 1 as one, 'foo' as two",
				Outputs: []codegen.OutputColumn{
					{PgName: "one", Type: pg.Int4, Nullable: false},
					{PgName: "two", Type: pg.Text, Nullable: false},
				},
			},
		},
		{
			name: "union one col",
			query: &ast.SourceQuery{
				Name:        "UnionOneCol",
				PreparedSQL: "SELECT 1 AS num UNION SELECT 2 AS num",
				ResultKind:  ast.ResultKindMany,
			},
			want: codegen.TypedQuery{
				Name:        "UnionOneCol",
				ResultKind:  ast.ResultKindMany,
				PreparedSQL: "SELECT 1 AS num UNION SELECT 2 AS num",
				Outputs: []codegen.OutputColumn{
					{PgName: "num", Type: pg.Int4, Nullable: true},
				},
			},
		},
		{
			name: "one col domain type",
			query: &ast.SourceQuery{
				Name:        "Domain",
				PreparedSQL: "SELECT '94109'::us_postal_code",
				ResultKind:  ast.ResultKindOne,
			},
			want: codegen.TypedQuery{
				Name:        "Domain",
				ResultKind:  ast.ResultKindOne,
				PreparedSQL: "SELECT '94109'::us_postal_code",
				Outputs: []codegen.OutputColumn{{
					PgName:   "us_postal_code",
					Type:     pg.Text,
					Nullable: false,
				}},
			},
		},
		{
			name: "one col domain type",
			query: &ast.SourceQuery{
				Name: "UnionEnumArrays",
				PreparedSQL: texts.Dedent(`
					SELECT enum_range('phone'::device_type, 'phone'::device_type) AS device_types
					UNION ALL
					SELECT enum_range(NULL::device_type) AS device_types;
				`),
				ResultKind: ast.ResultKindMany,
			},
			want: codegen.TypedQuery{
				Name:       "UnionEnumArrays",
				ResultKind: ast.ResultKindMany,
				PreparedSQL: texts.Dedent(`
					SELECT enum_range('phone'::device_type, 'phone'::device_type) AS device_types
					UNION ALL
					SELECT enum_range(NULL::device_type) AS device_types;
				`),
				Outputs: []codegen.OutputColumn{
					{
						PgName: "device_types",
						Type: pg.ArrayType{
							ID:   deviceTypeArrOID,
							Name: "_device_type",
							Elem: pg.EnumType{
								ID:     deviceTypeOID,
								Name:   "device_type",
								Labels: []string{"phone", "laptop"},
								Orders: []float32{1, 2},
							},
						},
						Nullable: true,
					},
				},
			},
		},
		{
			name: "find by first name",
			query: &ast.SourceQuery{
				Name:        "FindByFirstName",
				PreparedSQL: "SELECT first_name FROM author WHERE first_name = $1;",
				ParamNames:  []string{"FirstName"},
				ResultKind:  ast.ResultKindMany,
				Doc:         newCommentGroup("--   Hello  ", "-- name: Foo"),
			},
			want: codegen.TypedQuery{
				Name:        "FindByFirstName",
				ResultKind:  ast.ResultKindMany,
				Doc:         []string{"Hello"},
				PreparedSQL: "SELECT first_name FROM author WHERE first_name = $1;",
				Inputs: []codegen.InputParam{
					{PgName: "FirstName", Type: pg.Text},
				},
				Outputs: []codegen.OutputColumn{
					{PgName: "first_name", Type: pg.Text, Nullable: false},
				},
			},
		},
		{
			name: "find by first name join",
			query: &ast.SourceQuery{
				Name:        "FindByFirstNameJoin",
				PreparedSQL: "SELECT a1.first_name FROM author a1 JOIN author a2 USING (author_id) WHERE a1.first_name = $1;",
				ParamNames:  []string{"FirstName"},
				ResultKind:  ast.ResultKindMany,
				Doc:         newCommentGroup("--   Hello  ", "-- name: Foo"),
			},
			want: codegen.TypedQuery{
				Name:        "FindByFirstNameJoin",
				ResultKind:  ast.ResultKindMany,
				Doc:         []string{"Hello"},
				PreparedSQL: "SELECT a1.first_name FROM author a1 JOIN author a2 USING (author_id) WHERE a1.first_name = $1;",
				Inputs: []codegen.InputParam{
					{PgName: "FirstName", Type: pg.Text},
				},
				Outputs: []codegen.OutputColumn{
					{PgName: "first_name", Type: pg.Text, Nullable: true},
				},
			},
		},
		{
			name: "delete by author ID",
			query: &ast.SourceQuery{
				Name:        "DeleteAuthorByID",
				PreparedSQL: "DELETE FROM author WHERE author_id = $1;",
				ParamNames:  []string{"AuthorID"},
				ResultKind:  ast.ResultKindExec,
				Doc:         newCommentGroup("-- One", "--- - two", "-- name: Foo"),
			},
			want: codegen.TypedQuery{
				Name:        "DeleteAuthorByID",
				ResultKind:  ast.ResultKindExec,
				Doc:         []string{"One", "- two"},
				PreparedSQL: "DELETE FROM author WHERE author_id = $1;",
				Inputs: []codegen.InputParam{
					{PgName: "AuthorID", Type: pg.Int4},
				},
				Outputs: nil,
			},
		},
		{
			name: "delete by author id returning",
			query: &ast.SourceQuery{
				Name:        "DeleteAuthorByIDReturning",
				PreparedSQL: "DELETE FROM author WHERE author_id = $1 RETURNING author_id, first_name, suffix;",
				ParamNames:  []string{"AuthorID"},
				ResultKind:  ast.ResultKindMany,
			},
			want: codegen.TypedQuery{
				Name:        "DeleteAuthorByIDReturning",
				ResultKind:  ast.ResultKindMany,
				PreparedSQL: "DELETE FROM author WHERE author_id = $1 RETURNING author_id, first_name, suffix;",
				Inputs: []codegen.InputParam{
					{PgName: "AuthorID", Type: pg.Int4},
				},
				Outputs: []codegen.OutputColumn{
					{PgName: "author_id", Type: pg.Int4, Nullable: false},
					{PgName: "first_name", Type: pg.Text, Nullable: false},
					{PgName: "suffix", Type: pg.Text, Nullable: true},
				},
			},
		},
		{
			name: "update by author id returning",
			query: &ast.SourceQuery{
				Name:        "UpdateByAuthorIDReturning",
				PreparedSQL: "UPDATE author set first_name = 'foo' WHERE author_id = $1 RETURNING author_id, first_name, suffix;",
				ParamNames:  []string{"AuthorID"},
				ResultKind:  ast.ResultKindMany,
			},
			want: codegen.TypedQuery{
				Name:        "UpdateByAuthorIDReturning",
				ResultKind:  ast.ResultKindMany,
				PreparedSQL: "UPDATE author set first_name = 'foo' WHERE author_id = $1 RETURNING author_id, first_name, suffix;",
				Inputs: []codegen.InputParam{
					{PgName: "AuthorID", Type: pg.Int4},
				},
				Outputs: []codegen.OutputColumn{
					{PgName: "author_id", Type: pg.Int4, Nullable: false},
					{PgName: "first_name", Type: pg.Text, Nullable: false},
					{PgName: "suffix", Type: pg.Text, Nullable: true},
				},
			},
		},
		{
			name: "void one",
			query: &ast.SourceQuery{
				Name:        "VoidOne",
				PreparedSQL: "SELECT ''::void;",
				ParamNames:  []string{},
				ResultKind:  ast.ResultKindExec,
			},
			want: codegen.TypedQuery{
				Name:        "VoidOne",
				ResultKind:  ast.ResultKindExec,
				PreparedSQL: "SELECT ''::void;",
				Inputs:      nil,
				Outputs: []codegen.OutputColumn{
					{PgName: "void", Type: pg.Void, Nullable: false},
				},
			},
		},
		{
			name: "void two",
			query: &ast.SourceQuery{
				Name:        "VoidTwo",
				PreparedSQL: "SELECT 'foo' as foo, ''::void;",
				ParamNames:  []string{},
				ResultKind:  ast.ResultKindOne,
			},
			want: codegen.TypedQuery{
				Name:        "VoidTwo",
				ResultKind:  ast.ResultKindOne,
				PreparedSQL: "SELECT 'foo' as foo, ''::void;",
				Inputs:      nil,
				Outputs: []codegen.OutputColumn{
					{PgName: "foo", Type: pg.Text, Nullable: false},
					{PgName: "void", Type: pg.Void, Nullable: false},
				},
			},
		},
		{
			"pragma proto type",
			&ast.SourceQuery{
				Name:        "PragmaProtoType",
				PreparedSQL: "SELECT 1 as one, 'foo' as two",
				ResultKind:  ast.ResultKindOne,
				Pragmas:     ast.Pragmas{ProtobufType: "foo.Bar"},
			},
			codegen.TypedQuery{
				Name:        "PragmaProtoType",
				ResultKind:  ast.ResultKindOne,
				PreparedSQL: "SELECT 1 as one, 'foo' as two",
				Outputs: []codegen.OutputColumn{
					{PgName: "one", Type: pg.Int4, Nullable: false},
					{PgName: "two", Type: pg.Text, Nullable: false},
				},
				ProtobufType: "foo.Bar",
			},
		},
		{
			name: "aggregate non-null column has null output",
			query: &ast.SourceQuery{
				Name:        "ArrayAggFirstName",
				PreparedSQL: "SELECT array_agg(first_name) AS names FROM author;",
				ParamNames:  []string{},
				ResultKind:  ast.ResultKindOne,
				Doc:         newCommentGroup("--   Hello  ", "-- name: Foo"),
			},
			want: codegen.TypedQuery{
				Name:        "ArrayAggFirstName",
				ResultKind:  ast.ResultKindOne,
				Doc:         []string{"Hello"},
				PreparedSQL: "SELECT array_agg(first_name) AS names FROM author;",
				Inputs:      []codegen.InputParam{},
				Outputs: []codegen.OutputColumn{
					{PgName: "names", Type: pg.TextArray, Nullable: true},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inferrer := NewInferrer(conn)
			got, err := inferrer.InferTypes(tt.query)
			if err != nil {
				t.Fatal(err)
			}
			opts := cmp.Options{
				cmpopts.IgnoreFields(pg.EnumType{}, "ChildOIDs"),
			}
			difftest.AssertSame(t, tt.want, got, opts)
		})
	}
}

func TestInferrer_InferTypes_Error(t *testing.T) {
	conn := pgtest.NewPostgresSchema(t, []string{
		"../../example/author/schema.sql",
	})

	tests := []struct {
		query *ast.SourceQuery
		want  error
	}{
		{
			&ast.SourceQuery{
				Name:        "DeleteAuthorByIDMany",
				PreparedSQL: "DELETE FROM author WHERE author_id = $1;",
				ParamNames:  []string{"AuthorID"},
				ResultKind:  ast.ResultKindMany,
			},
			errors.New("query DeleteAuthorByIDMany has incompatible result kind :many; " +
				"the query doesn't return any columns; " +
				"use :exec if query shouldn't return any columns"),
		},
		{
			&ast.SourceQuery{
				Name:        "DeleteAuthorByIDOne",
				PreparedSQL: "DELETE FROM author WHERE author_id = $1;",
				ParamNames:  []string{"AuthorID"},
				ResultKind:  ast.ResultKindOne,
			},
			errors.New(
				"query DeleteAuthorByIDOne has incompatible result kind :one; " +
					"the query doesn't return any columns; " +
					"use :exec if query shouldn't return any columns"),
		},
		{
			&ast.SourceQuery{
				Name:        "VoidOne",
				PreparedSQL: "SELECT ''::void;",
				ParamNames:  nil,
				ResultKind:  ast.ResultKindMany,
			},
			errors.New(
				"query VoidOne has incompatible result kind :many; " +
					"the query only has void columns; " +
					"use :exec if query shouldn't return any columns"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.query.Name, func(t *testing.T) {
			inferrer := NewInferrer(conn)
			got, err := inferrer.InferTypes(tt.query)
			assert.Equal(t, codegen.TypedQuery{}, got, "InferTypes should error and return empty TypedQuery struct")
			assert.Equal(t, tt.want, err, "InferType error should match")
		})
	}
}

func newCommentGroup(lines ...string) *ast.CommentGroup {
	cs := make([]*ast.LineComment, len(lines))
	for i, line := range lines {
		cs[i] = &ast.LineComment{Text: line}
	}
	return &ast.CommentGroup{List: cs}
}
