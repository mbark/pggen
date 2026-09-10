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
		// An Identifier is substituted into the query text, not bound; see
		// EmitChIdentifierPrelude.
		if tq.isIdentifier(input) {
			continue
		}
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
		// A paginated query runs as its variants, so those are the ones whose
		// method the connection has to have.
		for _, q := range append(append([]TemplatedQuery{}, file.Queries...), file.Variants...) {
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
//
// A paginated query is emitted as its variants, so those count too — the
// dispatcher itself names no parameters.
func (tf TemplatedFile) needsClickHouseImport() bool {
	for _, q := range append(append([]TemplatedQuery{}, tf.Queries...), tf.Variants...) {
		if len(q.Inputs) > 0 {
			return true
		}
	}
	return false
}

// EmitChVariantParamNames emits a paginate variant's arguments, read from the
// group's unified params struct.
//
// A variant is a private helper the dispatcher calls, so every variant takes
// the same struct even though each uses a different subset of its fields —
// which is what the cursor arguments of the sort key it was fanned out for
// amount to.
func (tq TemplatedQuery) EmitChVariantParamNames() string {
	sb := &strings.Builder{}
	for _, input := range tq.Inputs {
		if tq.isIdentifier(input) {
			continue
		}
		sb.WriteString(",\n\t\tclickhouse.Named(")
		sb.WriteString(strconv.Quote(input.RawName.PgName))
		sb.WriteString(", params.")
		sb.WriteString(input.UpperName)
		sb.WriteString(")")
	}
	if sb.Len() > 0 {
		sb.WriteString(",\n\t")
	}
	return sb.String()
}

// EmitChVariantIdentifierPrelude is EmitChIdentifierPrelude for a paginate
// variant, which always reads its parameters from the group's unified struct
// however few of them it uses.
func (tq TemplatedQuery) EmitChVariantIdentifierPrelude() (string, error) {
	return tq.chIdentifierPrelude(true)
}

// isIdentifier reports whether input is an {…:Identifier} parameter.
func (tq TemplatedQuery) isIdentifier(input TemplatedParam) bool {
	chType, ok := input.RawName.Type.(ch.Type)
	return ok && ch.IsIdentifier(chType)
}

// chIdentifierInputs are the query's {…:Identifier} parameters, in order.
func (tq TemplatedQuery) chIdentifierInputs() []TemplatedParam {
	var out []TemplatedParam
	for _, input := range tq.Inputs {
		if tq.isIdentifier(input) {
			out = append(out, input)
		}
	}
	return out
}

// EmitChHasIdentifiers reports whether the query names a table or column
// through an {…:Identifier} parameter.
func (tq TemplatedQuery) EmitChHasIdentifiers() bool {
	return len(tq.chIdentifierInputs()) > 0
}

// EmitChSQLRef is what the query call passes as its SQL: the constant, or the
// local the identifier substitution produced.
func (tq TemplatedQuery) EmitChSQLRef() string {
	if tq.EmitChHasIdentifiers() {
		return "sql"
	}
	return tq.SQLVarName
}

// EmitChIdentifierPrelude emits the substitution that fills a query's
// {…:Identifier} holes before it is sent.
//
// It has to happen here and not in the driver. ClickHouse binds an Identifier
// itself, and safely, but clickhouse-go switches a query to server-side
// parameters as soon as its text holds any {…:…} — and server-side parameters
// travel as text the driver renders wrongly for time.Time, uuid.UUID and
// decimal.Decimal. Leaving one Identifier for the server would silently break
// every other parameter in the same query.
func (tq TemplatedQuery) EmitChIdentifierPrelude() (string, error) {
	return tq.chIdentifierPrelude(!tq.isInlineParams())
}

func (tq TemplatedQuery) chIdentifierPrelude(fromStruct bool) (string, error) {
	idents := tq.chIdentifierInputs()
	if len(idents) == 0 {
		return "", nil
	}

	sb := &strings.Builder{}
	sb.WriteString("\n\tsql, err := substituteIdentifiers(")
	sb.WriteString(tq.SQLVarName)
	sb.WriteString(", map[string]string{")
	for i, input := range idents {
		if i > 0 {
			sb.WriteString(", ")
		}
		value := input.LowerName
		if fromStruct {
			value = "params." + input.UpperName
		}
		sb.WriteString(strconv.Quote(input.RawName.PgName))
		sb.WriteString(": ")
		sb.WriteString(value)
	}
	sb.WriteString("})\n\tif err != nil {\n\t\treturn ")

	zero, err := tq.emitChZeroResult()
	if err != nil {
		return "", err
	}
	sb.WriteString(zero)
	sb.WriteString("fmt.Errorf(")
	sb.WriteString(strconv.Quote("query " + tq.Name + ": %w"))
	sb.WriteString(", err)\n\t}")
	return sb.String(), nil
}

// emitChZeroResult is what a method returns alongside an error, ready to be
// followed by the error itself: "" for an :exec, "nil, " for a slice, and the
// declared item for a :one.
func (tq TemplatedQuery) emitChZeroResult() (string, error) {
	switch tq.ResultKind {
	case ast.ResultKindExec:
		return "", nil
	case ast.ResultKindOne:
		// The :one body declares item before the query runs, so name it.
		return "item, ", nil
	default:
		return "nil, ", nil
	}
}
