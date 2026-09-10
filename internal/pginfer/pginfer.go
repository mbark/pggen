package pginfer

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mbark/pggen/internal/ast"
	"github.com/mbark/pggen/internal/codegen"
	"github.com/mbark/pggen/internal/pg"
)

const defaultTimeout = 3 * time.Second

type Inferrer struct {
	conn        *pgx.Conn
	typeFetcher *pg.TypeFetcher
}

// NewInferrer infers information about a query by running the query on
// Postgres and extracting information from the catalog tables.
func NewInferrer(conn *pgx.Conn) *Inferrer {
	return &Inferrer{
		conn:        conn,
		typeFetcher: pg.NewTypeFetcher(conn),
	}
}

func (inf *Inferrer) InferTypes(query *ast.SourceQuery) (codegen.TypedQuery, error) {
	inputs, outputs, err := inf.prepareTypes(query)
	if err != nil {
		return codegen.TypedQuery{}, fmt.Errorf("infer output types for query: %w", err)
	}
	if query.ResultKind != ast.ResultKindExec && len(outputs) == 0 {
		return codegen.TypedQuery{}, fmt.Errorf(
			"query %s has incompatible result kind %s; the query doesn't return any columns; "+
				"use :exec if query shouldn't return any columns",
			query.Name, query.ResultKind)
	}
	if query.ResultKind != ast.ResultKindExec && countVoids(outputs) == len(outputs) {
		return codegen.TypedQuery{}, fmt.Errorf(
			"query %s has incompatible result kind %s; the query only has void columns; "+
				"use :exec if query shouldn't return any columns",
			query.Name, query.ResultKind)
	}
	doc := codegen.ExtractDoc(query)
	return codegen.TypedQuery{
		Name:         query.Name,
		ResultKind:   query.ResultKind,
		Doc:          doc,
		PreparedSQL:  query.PreparedSQL,
		Inputs:       inputs,
		Outputs:      outputs,
		ProtobufType: query.Pragmas.ProtobufType,
		OutputType:   query.Pragmas.OutputType,
		SQLConst:     query.Pragmas.SQLConst,
		VariantGroup: query.VariantGroup,
		VariantKey:   query.VariantKey,
	}, nil
}

func (inf *Inferrer) prepareTypes(query *ast.SourceQuery) (_a []codegen.InputParam, _ []codegen.OutputColumn, mErr error) {
	// Execute the query to get field descriptions of the output columns.
	ctx, cancel := context.WithTimeout(context.Background(), defaultTimeout)
	defer cancel()

	// If paramOIDs is null, Postgres infers the type for each parameter.
	var paramOIDs []uint32
	stmtDesc, err := inf.conn.PgConn().Prepare(ctx, "", query.PreparedSQL, paramOIDs)
	if err != nil {
		if pgErr, ok := err.(*pgconn.PgError); ok {
			msg := "fetch field descriptions: " + pgErr.Message
			if pgErr.Where != "" {
				msg += "\n    WHERE: " + pgErr.Where
			}
			if pgErr.Detail != "" {
				msg += "\n    DETAIL: " + pgErr.Detail
			}
			if pgErr.Hint != "" {
				msg += "\n    HINT: " + pgErr.Hint
			}
			if pgErr.DataTypeName != "" {
				msg += "\n    DataType: " + pgErr.DataTypeName
			}
			if pgErr.TableName != "" {
				msg += "\n    TableName: " + pgErr.TableName
			}
			// Provide hint to use a returning clause. pggen ignores most errors but
			// only if there's output columns. If the user has an UPDATE or INSERT
			// without a RETURNING clause, pggen will surface the null constraint
			// errors because len(descriptions) == 0.
			if strings.Contains(strings.ToLower(query.PreparedSQL), "update") ||
				strings.Contains(strings.ToLower(query.PreparedSQL), "insert") {
				msg += "\n    HINT: if the main statement is an UPDATE or INSERT ensure that you have"
				msg += "\n          a RETURNING clause (this query is marked " + string(query.ResultKind) + ")."
				msg += "\n          Use :exec if you don't need the query output."
			}
			return nil, nil, fmt.Errorf(msg+"\n    %w", pgErr)
		}
		return nil, nil, fmt.Errorf("prepare query to infer types: %w", err)
	}

	// Validate.
	if len(stmtDesc.ParamOIDs) != len(query.ParamNames) {
		return nil, nil, fmt.Errorf("expected %d parameter types for query; got %d", len(query.ParamNames), len(stmtDesc.ParamOIDs))
	}

	// Build input params.
	var inputParams []codegen.InputParam
	if len(stmtDesc.ParamOIDs) > 0 {
		types, err := inf.typeFetcher.FindTypesByOIDs(stmtDesc.ParamOIDs...)
		if err != nil {
			return nil, nil, fmt.Errorf("fetch oid types: %w", err)
		}
		for i, oid := range stmtDesc.ParamOIDs {
			inputType, ok := types[oid]
			if !ok {
				return nil, nil, fmt.Errorf("no postgres type name found for parameter %s with oid %d", query.ParamNames[i], oid)
			}
			inputParams = append(inputParams, codegen.InputParam{
				PgName: query.ParamNames[i],
				Type:   inputType,
			})
		}
	}

	// Resolve type names of output column data type OIDs.
	outputOIDs := make([]uint32, len(stmtDesc.Fields))
	for i, desc := range stmtDesc.Fields {
		outputOIDs[i] = desc.DataTypeOID
	}
	outputTypes, err := inf.typeFetcher.FindTypesByOIDs(outputOIDs...)
	if err != nil {
		return nil, nil, fmt.Errorf("fetch oid types: %w", err)
	}

	// Output nullability.
	nullables, err := inf.inferOutputNullability(query, stmtDesc.Fields)
	if err != nil {
		return nil, nil, fmt.Errorf("infer output type nullability: %w", err)
	}

	// Create output columns
	var outputColumns []codegen.OutputColumn
	for i, desc := range stmtDesc.Fields {
		pgType, ok := outputTypes[desc.DataTypeOID]
		if !ok {
			return nil, nil, fmt.Errorf("no postgrestype name found for column %s with oid %d", desc.Name, desc.DataTypeOID)
		}
		outputColumns = append(outputColumns, codegen.OutputColumn{
			PgName:   desc.Name,
			Type:     pgType,
			Nullable: nullables[i],
		})
	}
	return inputParams, outputColumns, nil
}

// inferOutputNullability infers which of the output columns produced by the
// query and described by descs can be null.
func (inf *Inferrer) inferOutputNullability(query *ast.SourceQuery, descs []pgconn.FieldDescription) ([]bool, error) {
	if len(descs) == 0 {
		return nil, nil
	}
	plan, err := inf.explainQuery(query)
	if err != nil {
		return nil, err
	}

	columnKeys := make([]pg.ColumnKey, len(descs))
	for i, desc := range descs {
		if desc.TableOID > 0 {
			columnKeys[i] = pg.ColumnKey{
				TableOID: desc.TableOID,
				Number:   desc.TableAttributeNumber,
			}
		}
	}
	cols, err := pg.FetchColumns(inf.conn, columnKeys)
	if err != nil {
		return nil, fmt.Errorf("fetch column for nullability: %w", err)
	}

	// The nth entry determines if the output column described by descs[n] is
	// nullable. plan.Outputs might contain more entries than cols because the
	// plan output also contains information like sort columns.
	nullables := make([]bool, len(descs))
	for i := range nullables {
		nullables[i] = true // assume nullable until proven otherwise
	}
	for i, col := range cols {
		if i == len(plan.Outputs) {
			// plan.Outputs might not have the same output because the top level node
			// joins child outputs like with append.
			break
		}
		nullables[i] = isColNullable(query, plan, plan.Outputs[i], col)
	}
	return nullables, nil
}

func createParamArgs(query *ast.SourceQuery) []interface{} {
	args := make([]interface{}, len(query.ParamNames))
	for i := range query.ParamNames {
		args[i] = nil
	}
	return args
}

func countVoids(outputs []codegen.OutputColumn) int {
	n := 0
	for _, out := range outputs {
		if _, ok := out.Type.(pg.VoidType); ok {
			n++
		}
	}
	return n
}
