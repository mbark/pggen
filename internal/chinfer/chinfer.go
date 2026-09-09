// Package chinfer infers the parameter and result types of a ClickHouse query.
//
// It is the ClickHouse counterpart to internal/pginfer, and the two learn a
// query's shape in quite different ways. pginfer PREPAREs the query and reads
// back OIDs that Postgres inferred on its own, then guesses nullability with
// heuristics. ClickHouse has no PREPARE, but it does have DESCRIBE, which
// analyses a query and reports its result columns without running it — and
// because ClickHouse spells nullability in the type itself, Nullable(String),
// the answer is exact rather than guessed. Parameters need no round trip at
// all: {name:Type} already carries the type.
package chinfer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/ClickHouse/clickhouse-go/v2/lib/proto"
	"github.com/mbark/pggen/internal/ast"
	"github.com/mbark/pggen/internal/ch"
	"github.com/mbark/pggen/internal/codegen"
)

const defaultTimeout = 10 * time.Second

// describeParamPrefix names the substitute parameters that inference binds.
// See describe for why the query's own names cannot be used.
const describeParamPrefix = "pggen_param_"

// Inferrer infers types by running DESCRIBE on a live ClickHouse.
type Inferrer struct {
	conn     driver.Conn
	settings clickhouse.Settings
}

// Option configures an Inferrer.
type Option func(*Inferrer)

// WithSettings applies ClickHouse settings to the DESCRIBE that infers result
// types.
//
// This matters more than it looks. Some settings change what a query returns:
// with join_use_nulls=0, the default, columns from the right side of a LEFT
// JOIN are *not* Nullable — unmatched rows get type defaults like ” and 0
// instead of NULL. Inference is only correct if it sees the same settings the
// application will connect with.
func WithSettings(s clickhouse.Settings) Option {
	return func(inf *Inferrer) { inf.settings = s }
}

// NewInferrer infers information about a query by asking ClickHouse to
// describe it.
func NewInferrer(conn driver.Conn, opts ...Option) *Inferrer {
	inf := &Inferrer{conn: conn}
	for _, opt := range opts {
		opt(inf)
	}
	return inf
}

// InferTypes returns the typed form of query.
func (inf *Inferrer) InferTypes(query *ast.SourceQuery) (codegen.TypedQuery, error) {
	params, err := ch.ScanParams(query.PreparedSQL)
	if err != nil {
		return codegen.TypedQuery{}, fmt.Errorf("query %s: %w", query.Name, err)
	}
	inputs := make([]codegen.InputParam, len(params))
	for i, p := range params {
		inputs[i] = codegen.InputParam{PgName: p.Name, Type: p.Type}
	}

	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	var outputs []codegen.OutputColumn
	if query.ResultKind == ast.ResultKindExec {
		// An :exec query has no result columns to infer, but it still gets
		// checked. EXPLAIN AST parses the query without running it, which is
		// as far as the server will go: EXPLAIN proper is SELECT-only, so
		// there is no way to have an INSERT semantically analysed short of
		// executing it. That is the price of not needing S3 credentials at
		// generation time for the INSERT ... SELECT FROM s3(...) imports.
		if err := inf.checkSyntax(ctx, query.PreparedSQL, params); err != nil {
			return codegen.TypedQuery{}, fmt.Errorf("check query %s: %w", query.Name, err)
		}
	} else {
		outputs, err = inf.describe(ctx, query.PreparedSQL, params)
		if err != nil {
			return codegen.TypedQuery{}, fmt.Errorf("infer output types for query %s: %w", query.Name, err)
		}
		if len(outputs) == 0 {
			return codegen.TypedQuery{}, fmt.Errorf(
				"query %s has incompatible result kind %s; the query doesn't return any columns; "+
					"use :exec if query shouldn't return any columns",
				query.Name, query.ResultKind)
		}
	}

	return codegen.TypedQuery{
		Name:         query.Name,
		ResultKind:   query.ResultKind,
		Doc:          codegen.ExtractDoc(query),
		PreparedSQL:  query.PreparedSQL,
		Inputs:       inputs,
		Outputs:      outputs,
		ProtobufType: query.Pragmas.ProtobufType,
		OutputType:   query.Pragmas.OutputType,
		VariantGroup: query.VariantGroup,
		VariantKey:   query.VariantKey,
	}, nil
}

// prepare readies a query for a server round trip that analyses it without
// running it, returning the rewritten SQL and the arguments to send with it.
//
// The server parses the query, so every parameter must have a value even
// though none of them can affect the answer.
//
// The values travel as server-side parameters, which ClickHouse keeps in the
// same map as query settings, so a parameter named after a setting — `limit`
// and `offset` are both settings — poisons the request. Sending a renamed copy
// of the query sidesteps the whole namespace; the answer is the same either
// way, since the server reads the parameters' types and not their names.
func (inf *Inferrer) prepare(
	ctx context.Context, sql string, params []ch.Param,
) (context.Context, string, []any, error) {
	args := make([]any, 0, len(params))
	names := make(map[string]string, len(params))
	for i, p := range params {
		lit, err := ch.ZeroLiteral(p.Type)
		if err != nil {
			return ctx, "", nil, err
		}
		names[p.Name] = fmt.Sprintf("%s%d", describeParamPrefix, i)
		args = append(args, clickhouse.Named(names[p.Name], lit))
	}
	sql, err := ch.RenameParams(sql, func(name string) string { return names[name] })
	if err != nil {
		return ctx, "", nil, err
	}
	if len(inf.settings) > 0 {
		ctx = clickhouse.Context(ctx, clickhouse.WithSettings(inf.settings))
	}
	// A trailing semicolon is a syntax error once the query is wrapped in
	// DESCRIBE (...) or prefixed with EXPLAIN AST.
	return ctx, strings.TrimRight(sql, "; \t\r\n"), args, nil
}

// checkSyntax asks the server to parse the query without running it.
//
// It is a parse check and nothing more — EXPLAIN AST resolves no table or
// column name — so it catches a malformed query but not one that names a
// column that does not exist. For a query that returns rows, describe does the
// full semantic check; this is the fallback for the ones that do not.
func (inf *Inferrer) checkSyntax(ctx context.Context, sql string, params []ch.Param) error {
	ctx, sql, args, err := inf.prepare(ctx, sql, params)
	if err != nil {
		return err
	}
	rows, err := inf.conn.Query(ctx, "EXPLAIN AST "+sql, args...)
	if err != nil {
		return describeError(err)
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		// The AST itself says nothing pggen can use; only the error does.
	}
	return describeError(rows.Err())
}

// describe runs DESCRIBE on the query and reads the result column names and
// types back out.
func (inf *Inferrer) describe(ctx context.Context, sql string, params []ch.Param) ([]codegen.OutputColumn, error) {
	ctx, sql, args, err := inf.prepare(ctx, sql, params)
	if err != nil {
		return nil, err
	}

	// DESCRIBE wraps the query in parentheses. The closing paren goes on its
	// own line because a query can end in a -- comment, which would otherwise
	// swallow it.
	rows, err := inf.conn.Query(ctx, "DESCRIBE (\n"+sql+"\n)", args...)
	if err != nil {
		return nil, describeError(err)
	}
	defer func() { _ = rows.Close() }()

	// DESCRIBE has grown columns across ClickHouse versions, so find the two
	// that matter by name rather than assuming a width.
	cols := rows.Columns()
	nameIdx, typeIdx := -1, -1
	for i, col := range cols {
		switch col {
		case "name":
			nameIdx = i
		case "type":
			typeIdx = i
		}
	}
	if nameIdx < 0 || typeIdx < 0 {
		return nil, fmt.Errorf("DESCRIBE returned columns %v, expected it to include name and type", cols)
	}

	var outputs []codegen.OutputColumn
	for rows.Next() {
		cells := make([]string, len(cols))
		dest := make([]any, len(cols))
		for i := range cells {
			dest[i] = &cells[i]
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, fmt.Errorf("scan DESCRIBE row: %w", err)
		}
		name, typeName := cells[nameIdx], cells[typeIdx]
		typ, err := ch.Parse(typeName)
		if err != nil {
			return nil, fmt.Errorf("output column %q: %w", name, err)
		}
		outputs = append(outputs, codegen.OutputColumn{
			PgName: name,
			Type:   typ,
			// Exact, not heuristic: ClickHouse puts nullability in the type.
			Nullable: ch.IsNullable(typ),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, describeError(err)
	}
	return outputs, nil
}

// describeError unwraps a ClickHouse server exception into something that
// names the problem rather than just its code.
func describeError(err error) error {
	var ex *proto.Exception
	if errors.As(err, &ex) {
		return fmt.Errorf("clickhouse rejected the query (code %d): %s", ex.Code, ex.Message)
	}
	return err
}
