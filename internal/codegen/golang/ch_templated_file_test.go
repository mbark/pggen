package golang

import (
	"testing"

	"github.com/mbark/pggen/internal/ast"
	"github.com/mbark/pggen/internal/ch"
	"github.com/mbark/pggen/internal/codegen"
	"github.com/stretchr/testify/assert"
)

// TestEmitChGenericConn covers the interface a generated package asks its
// caller for. It matters that it stays narrow: a package of read-only queries
// that demanded Exec could not be handed a read-only connection, and a wrapper
// like a concurrency limiter would have to grow methods it does not use.
func TestEmitChGenericConn(t *testing.T) {
	rowStructQuery := TemplatedQuery{
		ResultKind: ast.ResultKindMany,
		Outputs: []TemplatedColumn{
			{PgName: "a", UpperName: "A", QualType: "string"},
			{PgName: "b", UpperName: "B", QualType: "int64"},
		},
	}
	singleColumnQuery := TemplatedQuery{
		ResultKind: ast.ResultKindMany,
		Outputs:    []TemplatedColumn{{PgName: "a", UpperName: "A", QualType: "string"}},
	}
	oneQuery := TemplatedQuery{ResultKind: ast.ResultKindOne, Outputs: singleColumnQuery.Outputs}
	execQuery := TemplatedQuery{ResultKind: ast.ResultKindExec}

	tests := []struct {
		name    string
		queries []TemplatedQuery
		want    string
	}{
		{
			name:    "only :many with a row struct needs Select",
			queries: []TemplatedQuery{rowStructQuery},
			want: "type genericConn interface {\n" +
				"\t// Select runs the query and scans every row into dest, a pointer to a\n" +
				"\t// slice of structs.\n" +
				"\tSelect(ctx context.Context, dest any, query string, args ...any) error\n" +
				"}",
		},
		{
			name:    "a single-column :many needs Query instead",
			queries: []TemplatedQuery{singleColumnQuery},
			want: "type genericConn interface {\n" +
				"\tQuery(ctx context.Context, query string, args ...any) (driver.Rows, error)\n" +
				"}",
		},
		{
			name:    ":one needs QueryRow",
			queries: []TemplatedQuery{oneQuery},
			want: "type genericConn interface {\n" +
				"\tQueryRow(ctx context.Context, query string, args ...any) driver.Row\n" +
				"}",
		},
		{
			name:    ":exec needs Exec",
			queries: []TemplatedQuery{execQuery},
			want: "type genericConn interface {\n" +
				"\tExec(ctx context.Context, query string, args ...any) error\n" +
				"}",
		},
		{
			name:    "all four together",
			queries: []TemplatedQuery{rowStructQuery, singleColumnQuery, oneQuery, execQuery},
			want: "type genericConn interface {\n" +
				"\t// Select runs the query and scans every row into dest, a pointer to a\n" +
				"\t// slice of structs.\n" +
				"\tSelect(ctx context.Context, dest any, query string, args ...any) error\n" +
				"\tQuery(ctx context.Context, query string, args ...any) (driver.Rows, error)\n" +
				"\tQueryRow(ctx context.Context, query string, args ...any) driver.Row\n" +
				"\tExec(ctx context.Context, query string, args ...any) error\n" +
				"}",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file := TemplatedFile{Queries: tt.queries}
			file.Pkg = TemplatedPackage{Files: []TemplatedFile{file}}

			got := file.EmitChGenericConn()
			assert.Contains(t, got, tt.want)

			// The driver package is imported only for the two methods that
			// name a type from it.
			methods := chConnMethodsOf(file.Pkg.Files)
			assert.Equal(t, methods.Query || methods.QueryRow, methods.needsDriverPkg())
		})
	}
}

// The interface covers the whole package, not one file, because every file
// shares the DBQuerier the leader declares.
func TestEmitChGenericConn_spansThePackage(t *testing.T) {
	reader := TemplatedFile{Queries: []TemplatedQuery{{
		ResultKind: ast.ResultKindMany,
		Outputs: []TemplatedColumn{
			{PgName: "a", UpperName: "A", QualType: "string"},
			{PgName: "b", UpperName: "B", QualType: "int64"},
		},
	}}}
	writer := TemplatedFile{Queries: []TemplatedQuery{{ResultKind: ast.ResultKindExec}}}
	reader.Pkg = TemplatedPackage{Files: []TemplatedFile{reader, writer}}

	got := reader.EmitChGenericConn()
	assert.Contains(t, got, "Select(ctx context.Context")
	assert.Contains(t, got, "Exec(ctx context.Context",
		"the other file's :exec query still needs Exec on the shared conn")
}

// TestNeedsClickHouseImport covers which queries make a file reference the
// clickhouse package. An import it does not use is a compile error in the
// generated code, and an Identifier parameter is not bound through
// clickhouse.Named — it is substituted into the SQL text.
func TestNeedsClickHouseImport(t *testing.T) {
	bound := TemplatedParam{RawName: codegen.InputParam{PgName: "from", Type: ch.Scalar{Name: "DateTime"}}}
	identifier := TemplatedParam{RawName: codegen.InputParam{PgName: "tbl", Type: ch.Scalar{Name: "Identifier"}}}

	tests := []struct {
		name string
		file TemplatedFile
		want bool
	}{
		{
			name: "no parameters at all",
			file: TemplatedFile{Queries: []TemplatedQuery{{}}},
			want: false,
		},
		{
			name: "a bound parameter is named",
			file: TemplatedFile{Queries: []TemplatedQuery{{Inputs: []TemplatedParam{bound}}}},
			want: true,
		},
		{
			name: "an Identifier alone is substituted, not named",
			file: TemplatedFile{Queries: []TemplatedQuery{{Inputs: []TemplatedParam{identifier}}}},
			want: false,
		},
		{
			name: "an Identifier beside a bound parameter",
			file: TemplatedFile{Queries: []TemplatedQuery{{Inputs: []TemplatedParam{identifier, bound}}}},
			want: true,
		},
		{
			name: "a paginate variant counts, since it is what runs",
			file: TemplatedFile{Variants: []TemplatedQuery{{Inputs: []TemplatedParam{bound}}}},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.file.needsClickHouseImport())
		})
	}
}
