package chinfer

import (
	"strings"
	"testing"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/go-cmp/cmp"
	"github.com/mbark/pggen/internal/ast"
	"github.com/mbark/pggen/internal/ch"
	"github.com/mbark/pggen/internal/chtest"
	"github.com/mbark/pggen/internal/codegen"
	"github.com/mbark/pggen/internal/texts"
)

// schema mirrors the type vocabulary of telness's parseup.cdr, which is the
// shape this backend has to handle in practice.
const schema = `
CREATE TABLE cdr (
    a_num           String,
    record_type     Enum8('MOC' = 1, 'SMO' = 2, 'GPRS' = 7),
    rat             LowCardinality(Nullable(String)),
    provider        LowCardinality(String),
    units           Int64,
    charge          Decimal(18, 6),
    start_date      DateTime('UTC'),
    end_date        Nullable(DateTime('UTC')),
    updated_at      DateTime64(3, 'UTC'),
    labels          Array(String),
    metadata        Map(String, String),
    subscription_id UUID,
    customer_id     Nullable(UUID),
    premium         Bool
) ENGINE = MergeTree() ORDER BY (provider, a_num)
`

func newInferrer(t *testing.T, opts ...Option) *Inferrer {
	t.Helper()
	conn, _, cleanup := chtest.NewClickHouseDBString(t, texts.Dedent(schema))
	t.Cleanup(cleanup)
	return NewInferrer(conn, opts...)
}

func TestInferrer_InferTypes(t *testing.T) {
	inf := newInferrer(t)

	tests := []struct {
		name  string
		query *ast.SourceQuery
		want  codegen.TypedQuery
	}{
		{
			name: "literal query",
			query: &ast.SourceQuery{
				Name:        "LiteralQuery",
				PreparedSQL: "SELECT 1 AS one, 'foo' AS two",
				ResultKind:  ast.ResultKindOne,
			},
			want: codegen.TypedQuery{
				Name:        "LiteralQuery",
				ResultKind:  ast.ResultKindOne,
				PreparedSQL: "SELECT 1 AS one, 'foo' AS two",
				Inputs:      []codegen.InputParam{},
				Outputs: []codegen.OutputColumn{
					{PgName: "one", Type: ch.Scalar{Name: "UInt8"}, Nullable: false},
					{PgName: "two", Type: ch.Scalar{Name: "String"}, Nullable: false},
				},
			},
		},
		{
			// Every column of the table, so the full vocabulary round trips
			// through DESCRIBE and the type parser.
			name: "all column types",
			query: &ast.SourceQuery{
				Name:        "SelectAll",
				PreparedSQL: "SELECT * FROM cdr",
				ResultKind:  ast.ResultKindMany,
			},
			want: codegen.TypedQuery{
				Name:        "SelectAll",
				ResultKind:  ast.ResultKindMany,
				PreparedSQL: "SELECT * FROM cdr",
				Inputs:      []codegen.InputParam{},
				Outputs: []codegen.OutputColumn{
					{PgName: "a_num", Type: ch.Scalar{Name: "String"}},
					{PgName: "record_type", Type: ch.Enum{
						Bits:   8,
						Labels: []string{"MOC", "SMO", "GPRS"},
						Values: []int16{1, 2, 7},
					}},
					{
						PgName:   "rat",
						Type:     ch.LowCardinality{Elem: ch.Nullable{Elem: ch.Scalar{Name: "String"}}},
						Nullable: true,
					},
					{PgName: "provider", Type: ch.LowCardinality{Elem: ch.Scalar{Name: "String"}}},
					{PgName: "units", Type: ch.Scalar{Name: "Int64"}},
					{PgName: "charge", Type: ch.Decimal{Precision: 18, Scale: 6}},
					{PgName: "start_date", Type: ch.DateTime{TZ: "UTC"}},
					{PgName: "end_date", Type: ch.Nullable{Elem: ch.DateTime{TZ: "UTC"}}, Nullable: true},
					{PgName: "updated_at", Type: ch.DateTime64{Precision: 3, TZ: "UTC"}},
					{PgName: "labels", Type: ch.Array{Elem: ch.Scalar{Name: "String"}}},
					{PgName: "metadata", Type: ch.Map{
						KeyType: ch.Scalar{Name: "String"},
						ValType: ch.Scalar{Name: "String"},
					}},
					{PgName: "subscription_id", Type: ch.Scalar{Name: "UUID"}},
					{PgName: "customer_id", Type: ch.Nullable{Elem: ch.Scalar{Name: "UUID"}}, Nullable: true},
					{PgName: "premium", Type: ch.Scalar{Name: "Bool"}},
				},
			},
		},
		{
			// The shape of get_data_usage_per_msisdn_parseup.sql: named
			// parameters bound only so DESCRIBE will parse the query.
			name: "params are read from the query text",
			query: &ast.SourceQuery{
				Name: "FindUsage",
				PreparedSQL: texts.Dedent(`
					SELECT a_num, sum(units) AS data_bytes
					FROM cdr
					WHERE a_num IN {msisdns:Array(String)}
					  AND start_date >= {from:DateTime}
					  AND start_date < {to:DateTime}
					GROUP BY a_num`),
				ResultKind: ast.ResultKindMany,
			},
			want: codegen.TypedQuery{
				Name:       "FindUsage",
				ResultKind: ast.ResultKindMany,
				Inputs: []codegen.InputParam{
					{PgName: "msisdns", Type: ch.Array{Elem: ch.Scalar{Name: "String"}}},
					{PgName: "from", Type: ch.DateTime{}},
					{PgName: "to", Type: ch.DateTime{}},
				},
				Outputs: []codegen.OutputColumn{
					{PgName: "a_num", Type: ch.Scalar{Name: "String"}},
					{PgName: "data_bytes", Type: ch.Scalar{Name: "Int64"}},
				},
			},
		},
		{
			// Aggregates change the result type, and DESCRIBE reports it.
			name: "aggregate result types",
			query: &ast.SourceQuery{
				Name: "Aggregates",
				PreparedSQL: texts.Dedent(`
					SELECT count() AS n,
					       max(end_date) AS last_end,
					       any(rat) AS a_rat,
					       toString(any(record_type)) AS rt
					FROM cdr`),
				ResultKind: ast.ResultKindOne,
			},
			want: codegen.TypedQuery{
				Name:       "Aggregates",
				ResultKind: ast.ResultKindOne,
				Inputs:     []codegen.InputParam{},
				Outputs: []codegen.OutputColumn{
					{PgName: "n", Type: ch.Scalar{Name: "UInt64"}},
					{
						PgName:   "last_end",
						Type:     ch.Nullable{Elem: ch.DateTime{TZ: "UTC"}},
						Nullable: true,
					},
					// any() over a LowCardinality(Nullable(T)) drops the
					// encoding but keeps the nullability.
					{PgName: "a_rat", Type: ch.Nullable{Elem: ch.Scalar{Name: "String"}}, Nullable: true},
					// Casting an enum away yields a plain String.
					{PgName: "rt", Type: ch.Scalar{Name: "String"}},
				},
			},
		},
		{
			// :exec needs no DESCRIBE at all, so it works even for a query
			// against a table function pggen could never execute here.
			name: "exec skips describe entirely",
			query: &ast.SourceQuery{
				Name: "ImportFromS3",
				PreparedSQL: texts.Dedent(`
					INSERT INTO cdr (a_num, units)
					SELECT c1, c2 FROM s3({s3_url:String}, 'CSV', 'c1 String, c2 Int64')`),
				ResultKind: ast.ResultKindExec,
			},
			want: codegen.TypedQuery{
				Name:       "ImportFromS3",
				ResultKind: ast.ResultKindExec,
				Inputs:     []codegen.InputParam{{PgName: "s3_url", Type: ch.Scalar{Name: "String"}}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := inf.InferTypes(tt.query)
			if err != nil {
				t.Fatalf("InferTypes returned error: %v", err)
			}
			// PreparedSQL is echoed through unchanged; only assert it where the
			// case bothers to state it.
			if tt.want.PreparedSQL == "" {
				tt.want.PreparedSQL = got.PreparedSQL
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("InferTypes mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestInferrer_leftJoinNullability pins down the behaviour that most surprises
// someone coming from Postgres: by default a LEFT JOIN does not make the right
// side's columns Nullable, because unmatched rows get type defaults instead of
// NULL. Turning join_use_nulls on changes the query's result types, which is
// why the inferrer has to run under the application's settings.
func TestInferrer_leftJoinNullability(t *testing.T) {
	query := &ast.SourceQuery{
		Name: "LeftJoin",
		PreparedSQL: texts.Dedent(`
			SELECT l.a_num AS a_num, r.units AS units
			FROM cdr AS l LEFT JOIN cdr AS r ON l.a_num = r.a_num`),
		ResultKind: ast.ResultKindMany,
	}

	t.Run("default settings do not promote to Nullable", func(t *testing.T) {
		got, err := newInferrer(t).InferTypes(query)
		if err != nil {
			t.Fatalf("InferTypes returned error: %v", err)
		}
		want := []codegen.OutputColumn{
			{PgName: "a_num", Type: ch.Scalar{Name: "String"}},
			{PgName: "units", Type: ch.Scalar{Name: "Int64"}},
		}
		if diff := cmp.Diff(want, got.Outputs); diff != "" {
			t.Errorf("outputs mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("join_use_nulls promotes to Nullable", func(t *testing.T) {
		inf := newInferrer(t, WithSettings(clickhouse.Settings{"join_use_nulls": 1}))
		got, err := inf.InferTypes(query)
		if err != nil {
			t.Fatalf("InferTypes returned error: %v", err)
		}
		want := []codegen.OutputColumn{
			{PgName: "a_num", Type: ch.Scalar{Name: "String"}},
			{PgName: "units", Type: ch.Nullable{Elem: ch.Scalar{Name: "Int64"}}, Nullable: true},
		}
		if diff := cmp.Diff(want, got.Outputs); diff != "" {
			t.Errorf("outputs mismatch (-want +got):\n%s", diff)
		}
	})
}

// TestInferrer_settingNamedParams pins down a collision that only shows up at
// inference. DESCRIBE needs a value for every parameter, and those travel as
// server-side parameters, which ClickHouse keeps in the same map as query
// settings. `limit` and `offset` are both real settings, so a query with a
// parameter of either name used to be rejected with "Cannot parse quoted
// string" — and so was every other query in the same file, since the collision
// happens while the settings are read, before the SQL is parsed. Inference
// therefore describes a renamed copy of the query.
//
// Generated code is unaffected either way: it binds client-side through
// cast(@name AS Type), so the query keeps its own parameter names.
func TestInferrer_settingNamedParams(t *testing.T) {
	inf := newInferrer(t)

	query := &ast.SourceQuery{
		Name: "PagedCDRs",
		PreparedSQL: texts.Dedent(`
			SELECT a_num, units
			FROM cdr
			WHERE provider IN {providers:Array(String)}
			ORDER BY a_num
			LIMIT {limit:UInt32} OFFSET {offset:UInt32}`),
		ResultKind: ast.ResultKindMany,
	}

	got, err := inf.InferTypes(query)
	if err != nil {
		t.Fatalf("InferTypes returned error: %v", err)
	}

	wantInputs := []codegen.InputParam{
		{PgName: "providers", Type: ch.Array{Elem: ch.Scalar{Name: "String"}}},
		{PgName: "limit", Type: ch.Scalar{Name: "UInt32"}},
		{PgName: "offset", Type: ch.Scalar{Name: "UInt32"}},
	}
	if diff := cmp.Diff(wantInputs, got.Inputs); diff != "" {
		t.Errorf("inputs mismatch (-want +got):\n%s", diff)
	}
	wantOutputs := []codegen.OutputColumn{
		{PgName: "a_num", Type: ch.Scalar{Name: "String"}},
		{PgName: "units", Type: ch.Scalar{Name: "Int64"}},
	}
	if diff := cmp.Diff(wantOutputs, got.Outputs); diff != "" {
		t.Errorf("outputs mismatch (-want +got):\n%s", diff)
	}

	// The renaming is confined to the DESCRIBE; what the generator emits still
	// carries the query's own parameter names.
	if !strings.Contains(got.PreparedSQL, "{limit:UInt32}") {
		t.Errorf("PreparedSQL lost the query's own parameter name:\n%s", got.PreparedSQL)
	}
}

// TestInferrer_trailingLineComment matters because DESCRIBE wraps the query in
// parentheses. With the closing paren on the same line, a query that ends in a
// -- comment swallowed it and ClickHouse reported a syntax error pointing
// nowhere near the comment.
func TestInferrer_trailingLineComment(t *testing.T) {
	inf := newInferrer(t)

	query := &ast.SourceQuery{
		Name: "CommentedCDRs",
		PreparedSQL: texts.Dedent(`
			SELECT a_num
			FROM cdr -- only the one column
			;`),
		ResultKind: ast.ResultKindMany,
	}

	got, err := inf.InferTypes(query)
	if err != nil {
		t.Fatalf("InferTypes returned error: %v", err)
	}
	wantOutputs := []codegen.OutputColumn{
		{PgName: "a_num", Type: ch.Scalar{Name: "String"}},
	}
	if diff := cmp.Diff(wantOutputs, got.Outputs); diff != "" {
		t.Errorf("outputs mismatch (-want +got):\n%s", diff)
	}
}

// TestInferrer_execIsChecked matters because an :exec query has no result
// columns to infer, so it would be easy to send it through untouched. EXPLAIN
// AST parses it without running it — as far as the server will go, since
// EXPLAIN proper is SELECT-only.
func TestInferrer_execIsChecked(t *testing.T) {
	inf := newInferrer(t)

	t.Run("a well-formed insert passes", func(t *testing.T) {
		query := &ast.SourceQuery{
			Name: "InsertCDR",
			PreparedSQL: texts.Dedent(`
				INSERT INTO cdr (a_num, units)
				SELECT {a_num:String}, {units:Int64}`),
			ResultKind: ast.ResultKindExec,
		}
		got, err := inf.InferTypes(query)
		if err != nil {
			t.Fatalf("InferTypes returned error: %v", err)
		}
		if len(got.Outputs) != 0 {
			t.Errorf("an :exec query should have no outputs, got %v", got.Outputs)
		}
		wantInputs := []codegen.InputParam{
			{PgName: "a_num", Type: ch.Scalar{Name: "String"}},
			{PgName: "units", Type: ch.Scalar{Name: "Int64"}},
		}
		if diff := cmp.Diff(wantInputs, got.Inputs); diff != "" {
			t.Errorf("inputs mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a malformed insert is rejected", func(t *testing.T) {
		query := &ast.SourceQuery{
			Name:        "BadInsert",
			PreparedSQL: "INSERT INTO cdr (a_num) SELCT 'x'",
			ResultKind:  ast.ResultKindExec,
		}
		_, err := inf.InferTypes(query)
		if err == nil {
			t.Fatal("InferTypes accepted a query ClickHouse cannot parse")
		}
		if !strings.Contains(err.Error(), "clickhouse rejected the query") {
			t.Errorf("error %q should say clickhouse rejected the query", err)
		}
	})
}

func TestInferrer_InferTypes_errors(t *testing.T) {
	inf := newInferrer(t)
	tests := []struct {
		name    string
		query   *ast.SourceQuery
		wantSub string
	}{
		{
			name: "describe rejects an invalid query",
			query: &ast.SourceQuery{
				Name:        "NotAnAggregate",
				PreparedSQL: "SELECT a_num, sum(units) FROM cdr",
				ResultKind:  ast.ResultKindMany,
			},
			wantSub: "clickhouse rejected the query",
		},
		{
			name: "unknown table",
			query: &ast.SourceQuery{
				Name:        "NoSuchTable",
				PreparedSQL: "SELECT * FROM does_not_exist",
				ResultKind:  ast.ResultKindMany,
			},
			wantSub: "clickhouse rejected the query",
		},
		{
			name: "identifier param cannot be inferred",
			query: &ast.SourceQuery{
				Name:        "IdentifierParam",
				PreparedSQL: "SELECT * FROM {tbl:Identifier}",
				ResultKind:  ast.ResultKindMany,
			},
			wantSub: "Identifier",
		},
		{
			name: "conflicting param types",
			query: &ast.SourceQuery{
				Name:        "Conflicting",
				PreparedSQL: "SELECT {x:UInt64} FROM cdr WHERE a_num = {x:String}",
				ResultKind:  ast.ResultKindMany,
			},
			wantSub: "declared as both",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := inf.InferTypes(tt.query)
			if err == nil {
				t.Fatalf("InferTypes succeeded; want an error")
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("error = %q; want it to contain %q", err, tt.wantSub)
			}
		})
	}
}
