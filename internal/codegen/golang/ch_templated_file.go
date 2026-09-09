package golang

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/mbark/pggen/internal/ast"
	"github.com/mbark/pggen/internal/ch"
	"github.com/mbark/pggen/internal/codegen"
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
	writeRowStructFields(sb, outs, codegen.DialectClickHouse)
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

// chConnMethods reports which genericConn methods a package's queries call.
type chConnMethods struct{ Select, Query, QueryRow, Exec bool }

// needsDriverPkg reports whether the interface names a type from
// clickhouse-go's driver package. Only Query and QueryRow do.
func (m chConnMethods) needsDriverPkg() bool { return m.Query || m.QueryRow }

// chConnMethodsOf reports the methods the queries in files call.
func chConnMethodsOf(files []TemplatedFile) chConnMethods {
	var m chConnMethods
	for _, file := range files {
		for _, q := range file.Queries {
			switch {
			case q.ResultKind == ast.ResultKindExec:
				m.Exec = true
			case q.ResultKind == ast.ResultKindOne:
				m.QueryRow = true
			case q.EmitChUsesRowStruct():
				m.Select = true
			default:
				// A :many over a single column cannot use struct scanning.
				m.Query = true
			}
		}
	}
	return m
}

// EmitChGenericConn emits the genericConn interface with only the methods the
// package's queries call.
//
// A package of read-only queries should not demand a connection that can Exec,
// so a caller can pass a narrower interface of its own — which is what makes a
// wrapper like a concurrency limiter or a tracer usable here.
func (tf TemplatedFile) EmitChGenericConn() string {
	m := chConnMethodsOf(tf.Pkg.Files)

	sb := &strings.Builder{}
	sb.WriteString("// genericConn is a connection to ClickHouse, like clickhouse.Conn or a\n")
	sb.WriteString("// decorator that wraps one.\n")
	sb.WriteString("type genericConn interface {\n")
	if m.Select {
		sb.WriteString("\t// Select runs the query and scans every row into dest, a pointer to a\n")
		sb.WriteString("\t// slice of structs.\n")
		sb.WriteString("\tSelect(ctx context.Context, dest any, query string, args ...any) error\n")
	}
	if m.Query {
		sb.WriteString("\tQuery(ctx context.Context, query string, args ...any) (driver.Rows, error)\n")
	}
	if m.QueryRow {
		sb.WriteString("\tQueryRow(ctx context.Context, query string, args ...any) driver.Row\n")
	}
	if m.Exec {
		sb.WriteString("\tExec(ctx context.Context, query string, args ...any) error\n")
	}
	sb.WriteString("}")
	return sb.String()
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
