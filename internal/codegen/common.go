// Package codegen contains the dialect-neutral representation of a parsed and
// type-inferred query, shared between the inferrers that produce it
// (internal/pginfer, internal/chinfer) and the language-specific code
// generators that consume it. Separate package to avoid dependency cycles.
package codegen

import (
	"strings"

	"github.com/mbark/pggen/internal/ast"
	"github.com/mbark/pggen/internal/sqltype"
)

// Dialect is the database a set of queries runs against. It decides how types
// are inferred and which driver the generated code is written against.
type Dialect string

const (
	DialectPostgres   Dialect = "postgres"
	DialectClickHouse Dialect = "clickhouse"
)

// QueryFile represents all SQL queries from a single file.
type QueryFile struct {
	SourcePath string       // absolute path to the source SQL query file
	Queries    []TypedQuery // the typed queries
}

// TypedQuery is an enriched form of ast.SourceQuery after running it on the
// database to get information about the query.
type TypedQuery struct {
	// Name of the query, from the comment preceding the query. Like 'FindAuthors'
	// in the source SQL: "-- name: FindAuthors :many"
	Name string
	// The result output kind, :one, :many, or :exec.
	ResultKind ast.ResultKind
	// The comment lines preceding the query, without the SQL comment syntax and
	// excluding the :name line.
	Doc []string
	// The SQL query, with pggen functions replaced by the dialect's parameter
	// syntax. Ready to run on the database.
	PreparedSQL string
	// The input parameters to the query.
	Inputs []InputParam
	// The output columns of the query.
	Outputs []OutputColumn
	// Qualified protocol buffer message type to use for each output row, like
	// "erp.api.Product". If empty, generate our own Row type.
	ProtobufType string
	// User-specified output row struct name, like "ItemRow". If set, multiple
	// queries can share the same output struct.
	OutputType string
	// Set when this query is one fanned-out statement of a paginate=<spec>
	// query. VariantGroup is the public dispatcher name; VariantKey identifies
	// the sort key + direction. Empty VariantGroup means an ordinary query.
	VariantGroup string
	VariantKey   ast.VariantKey
}

// InputParam is an input parameter for a query.
type InputParam struct {
	// Name of the param, like 'FirstName' in pggen.arg('FirstName').
	PgName string
	// The database type of this param as reported by the database.
	Type sqltype.Type
}

// OutputColumn is a single column output from a select query or returning
// clause in an update, insert, or delete query.
type OutputColumn struct {
	// Name of an output column, named by the database, like "foo" in
	// "SELECT 1 as foo".
	PgName string
	// The database type of the column as reported by the database.
	Type sqltype.Type
	// If the type can be null; depends on the query. In Postgres a column
	// defined with a NOT NULL constraint can still be null in the output with a
	// left join, and nullability is determined using rudimentary control-flow
	// analysis. ClickHouse reports nullability exactly, in the type itself.
	Nullable bool
}

// ExtractDoc returns the comment lines preceding a query, with the SQL comment
// syntax stripped and the trailing "-- name: Foo :exec" line dropped.
func ExtractDoc(query *ast.SourceQuery) []string {
	if query.Doc == nil || len(query.Doc.List) <= 1 {
		return nil
	}
	// Drop last line, like: "-- name: Foo :exec"
	lines := make([]string, len(query.Doc.List)-1)
	for i := range lines {
		comment := query.Doc.List[i].Text
		// TrimLeft to remove runs of dashes. TrimPrefix only removes fixed number.
		noDashes := strings.TrimLeft(comment, "-")
		lines[i] = strings.TrimSpace(noDashes)
	}
	return lines
}
