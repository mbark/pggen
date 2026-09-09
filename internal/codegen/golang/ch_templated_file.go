package golang

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mbark/pggen/internal/ast"
	"github.com/mbark/pggen/internal/ch"
)

// The emitters in this file back query_clickhouse.gotemplate. They are named
// apart from the Postgres ones rather than branching inside them, so the
// Postgres output stays byte-for-byte what it was.

// EmitChUsesRowStruct reports whether a query's result element is a generated
// struct rather than a single bare column. It decides between clickhouse-go's
// struct scanning and a plain Scan, because Select and ScanStruct only accept
// a struct destination.
func (tq TemplatedQuery) EmitChUsesRowStruct() bool {
	if tq.ResultKind == ast.ResultKindExec {
		return false
	}
	return tq.OutputType != "" || len(removeVoidColumns(tq.Outputs)) > 1
}

// EmitChResultElem is the Go type of a single result item.
func (tq TemplatedQuery) EmitChResultElem() (string, error) {
	outs := removeVoidColumns(tq.Outputs)
	switch {
	case tq.EmitChUsesRowStruct():
		return tq.rowTypeName(), nil
	case len(outs) == 1:
		return outs[0].QualType, nil
	default:
		return "", fmt.Errorf("query %s has no result columns; use :exec", tq.Name)
	}
}

// EmitChResultType is the Go type a query method returns, ignoring the error.
func (tq TemplatedQuery) EmitChResultType() (string, error) {
	elem, err := tq.EmitChResultElem()
	if err != nil {
		return "", err
	}
	if tq.ResultKind == ast.ResultKindMany {
		return "[]" + elem, nil
	}
	return elem, nil
}

// EmitChResultSignature is the result part of a method signature.
//
// ClickHouse has no analogue of pgconn.CommandTag — Exec reports only an
// error — so an :exec query returns a bare error rather than a pair.
func (tq TemplatedQuery) EmitChResultSignature() (string, error) {
	if tq.ResultKind == ast.ResultKindExec {
		return "error", nil
	}
	result, err := tq.EmitChResultType()
	if err != nil {
		return "", err
	}
	return "(" + result + ", error)", nil
}

// EmitChZeroResult is the value to return alongside an error.
func (tq TemplatedQuery) EmitChZeroResult() (string, error) {
	switch tq.ResultKind {
	case ast.ResultKindExec:
		return "", nil
	case ast.ResultKindMany:
		return "nil, ", nil
	}
	// :one returns the zero value of the element type, which the generated
	// code has already declared as "item".
	return "item, ", nil
}

// EmitChParamNames emits the query arguments.
//
// ClickHouse parameters are named rather than positional: the query says
// {msisdns:Array(String)}, and the value is bound to that name. The type is
// already in the SQL, so the server does the conversion.
func (tq TemplatedQuery) EmitChParamNames() string {
	sb := &strings.Builder{}
	for _, input := range tq.Inputs {
		value := "params." + input.UpperName
		if tq.isInlineParams() {
			value = input.LowerName
		}
		sb.WriteString(",\n\t\tclickhouse.Named(")
		sb.WriteString(strconv.Quote(input.RawName.PgName))
		sb.WriteString(", ")
		sb.WriteString(value)
		sb.WriteString(")")
	}
	if sb.Len() > 0 {
		sb.WriteString(",\n\t")
	}
	return sb.String()
}

// EmitChRowStruct emits the row struct for a query.
//
// It carries a ch tag as well as a json tag: clickhouse-go's Select and
// ScanStruct bind columns to fields by that tag, and they require a
// destination for every column the query returns.
func (tq TemplatedQuery) EmitChRowStruct() string {
	if !tq.EmitChUsesRowStruct() || tq.OutputType != "" {
		// A shared output= struct is emitted once by SharedRowDeclarer.
		return ""
	}
	outs := removeVoidColumns(tq.Outputs)
	sb := &strings.Builder{}
	sb.WriteString("\n\ntype ")
	sb.WriteString(tq.Name)
	sb.WriteString("Row struct {\n")

	maxNameLen, maxTypeLen := getLongestOutput(outs)
	maxTagLen := 0
	for _, out := range outs {
		if n := len(chTag(out.PgName)); n > maxTagLen {
			maxTagLen = n
		}
	}
	maxTagLen++ // 1 space to separate the ch tag from the json tag

	for _, out := range outs {
		sb.WriteString("\t")
		sb.WriteString(out.UpperName)
		sb.WriteString(strings.Repeat(" ", maxNameLen-len(out.UpperName)))
		sb.WriteString(out.QualType)
		sb.WriteString(strings.Repeat(" ", maxTypeLen-len(out.QualType)))
		tag := chTag(out.PgName)
		sb.WriteString("`")
		sb.WriteString(tag)
		sb.WriteString(strings.Repeat(" ", maxTagLen-len(tag)))
		sb.WriteString("json:")
		sb.WriteString(strconv.Quote(out.PgName))
		sb.WriteString("`")
		sb.WriteRune('\n')
	}
	sb.WriteString("}")
	return sb.String()
}

// EmitChPreparedSQL emits the SQL constant, with each {name:Type} rewritten to
// cast(@name AS Type) so the driver binds values client-side. See
// ch.RewriteParams for why that is necessary.
func (tq TemplatedQuery) EmitChPreparedSQL() (string, error) {
	sql, err := ch.RewriteParams(tq.PreparedSQL)
	if err != nil {
		return "", fmt.Errorf("query %s: %w", tq.Name, err)
	}
	if strings.ContainsRune(sql, '`') {
		return strconv.Quote(sql), nil
	}
	return "`" + sql + "`", nil
}

func chTag(pgName string) string {
	return "ch:" + strconv.Quote(pgName)
}

// needsClickHouseImport reports whether a file references the clickhouse
// package, which it does only to name parameters with clickhouse.Named.
func (tf TemplatedFile) needsClickHouseImport() bool {
	for _, q := range tf.Queries {
		if len(q.Inputs) > 0 {
			return true
		}
	}
	for _, q := range tf.Variants {
		if len(q.Inputs) > 0 {
			return true
		}
	}
	return false
}
