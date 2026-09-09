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

	var outputs []codegen.OutputColumn
	if query.ResultKind != ast.ResultKindExec {
		// :exec queries have no result columns, so they need no round trip at
		// all. That matters for the INSERT ... SELECT FROM s3(...) imports,
		// which would otherwise need S3 credentials at generation time.
		ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
		defer cancel()
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

// describe runs DESCRIBE on the query and reads the result column names and
// types back out.
func (inf *Inferrer) describe(ctx context.Context, sql string, params []ch.Param) ([]codegen.OutputColumn, error) {
	// DESCRIBE parses the query, so every parameter must have a value even
	// though none of them can affect the result columns.
	args := make([]any, 0, len(params))
	for _, p := range params {
		lit, err := ch.ZeroLiteral(p.Type)
		if err != nil {
			return nil, err
		}
		args = append(args, clickhouse.Named(p.Name, lit))
	}
	if len(inf.settings) > 0 {
		ctx = clickhouse.Context(ctx, clickhouse.WithSettings(inf.settings))
	}

	// DESCRIBE wraps the query in parentheses, where a trailing semicolon is a
	// syntax error.
	rows, err := inf.conn.Query(ctx, "DESCRIBE ("+strings.TrimRight(sql, "; \t\r\n")+")", args...)
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
