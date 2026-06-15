package parser

import (
	"strings"
	"testing"

	"github.com/mbark/pggen/internal/ast"
	gotok "go/token"
)

const paginateSrc = `
-- sort: payments_sort
--   key payment_date: payment_date, payment_id
--   nullable: payment_date
--   default: payment_id
--   cursor: payment_date=cursor_payment_date, payment_id=cursor_payment_id

-- name: List :many output=PaymentRow paginate=payments_sort
SELECT payment_id, payment_date
FROM p_payment
WHERE pggen.keyset('payments_sort')
ORDER BY pggen.orderby('payments_sort');
`

func parseQueriesByName(t *testing.T, src string) map[string]*ast.SourceQuery {
	t.Helper()
	f, err := ParseFile(gotok.NewFileSet(), "test.sql", src, 0)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	out := make(map[string]*ast.SourceQuery)
	for _, q := range f.Queries {
		sq, ok := q.(*ast.SourceQuery)
		if !ok {
			t.Fatalf("query is %T, want *ast.SourceQuery", q)
		}
		out[sq.Name] = sq
	}
	return out
}

func TestExpandPaginatedQueries_FansOutVariants(t *testing.T) {
	got := parseQueriesByName(t, paginateSrc)

	wantNames := []string{"List__default", "List__payment_date_desc", "List__payment_date_asc"}
	for _, n := range wantNames {
		if _, ok := got[n]; !ok {
			t.Fatalf("missing variant %q; got %v", n, keys(got))
		}
	}
	if len(got) != len(wantNames) {
		t.Fatalf("got %d queries, want %d: %v", len(got), len(wantNames), keys(got))
	}

	for _, sq := range got {
		if sq.VariantGroup != "List" {
			t.Errorf("%s: VariantGroup = %q, want List", sq.Name, sq.VariantGroup)
		}
		if strings.Contains(sq.PreparedSQL, "pggen.") {
			t.Errorf("%s: PreparedSQL still contains a pggen placeholder:\n%s", sq.Name, sq.PreparedSQL)
		}
		if sq.Pragmas.OutputType != "PaymentRow" {
			t.Errorf("%s: OutputType = %q, want PaymentRow", sq.Name, sq.Pragmas.OutputType)
		}
	}
}

func TestExpandPaginatedQueries_DescVariant(t *testing.T) {
	got := parseQueriesByName(t, paginateSrc)
	desc := got["List__payment_date_desc"]

	if desc.VariantKey != (ast.VariantKey{SortKey: "payment_date", Descending: true}) {
		t.Errorf("VariantKey = %+v", desc.VariantKey)
	}
	// Clean, index-usable ORDER BY.
	wantOrder := "ORDER BY payment_date DESC NULLS FIRST, payment_id DESC"
	if !strings.Contains(desc.PreparedSQL, wantOrder) {
		t.Errorf("missing %q in:\n%s", wantOrder, desc.PreparedSQL)
	}
	// NULL-aware keyset predicate over (payment_date, payment_id).
	for _, want := range []string{
		"payment_date IS NOT DISTINCT FROM $1 AND payment_id < $2",
		"payment_date < $1",
		"payment_date IS NOT NULL AND $1 IS NULL",
		"$1 IS NULL AND $2 IS NULL", // first-page escape
	} {
		if !strings.Contains(desc.PreparedSQL, want) {
			t.Errorf("missing %q in:\n%s", want, desc.PreparedSQL)
		}
	}
	wantParams := []string{"cursor_payment_date", "cursor_payment_id"}
	if !equalStrings(desc.ParamNames, wantParams) {
		t.Errorf("ParamNames = %v, want %v", desc.ParamNames, wantParams)
	}
}

func TestExpandPaginatedQueries_AscVariant(t *testing.T) {
	got := parseQueriesByName(t, paginateSrc)
	asc := got["List__payment_date_asc"]

	wantOrder := "ORDER BY payment_date ASC NULLS LAST, payment_id ASC"
	if !strings.Contains(asc.PreparedSQL, wantOrder) {
		t.Errorf("missing %q in:\n%s", wantOrder, asc.PreparedSQL)
	}
	for _, want := range []string{
		"payment_date IS NOT DISTINCT FROM $1 AND payment_id > $2",
		"payment_date > $1",
		"payment_date IS NULL AND $1 IS NOT NULL",
	} {
		if !strings.Contains(asc.PreparedSQL, want) {
			t.Errorf("missing %q in:\n%s", want, asc.PreparedSQL)
		}
	}
}

func TestExpandPaginatedQueries_DefaultVariant(t *testing.T) {
	got := parseQueriesByName(t, paginateSrc)
	def := got["List__default"]

	if !def.VariantKey.IsDefault {
		t.Errorf("VariantKey = %+v, want IsDefault", def.VariantKey)
	}
	if !strings.Contains(def.PreparedSQL, "ORDER BY payment_id ASC") {
		t.Errorf("missing default ORDER BY in:\n%s", def.PreparedSQL)
	}
	if !strings.Contains(def.PreparedSQL, "payment_id > $1") {
		t.Errorf("missing default keyset in:\n%s", def.PreparedSQL)
	}
	if !equalStrings(def.ParamNames, []string{"cursor_payment_id"}) {
		t.Errorf("ParamNames = %v", def.ParamNames)
	}
}

const orderingOnlySrc = `
-- sort: workorder_sort
--   key created_at: created_at
--   key execute_at: COALESCE(execute_at, activate_at, created_at)
--   nullable: activate_at
--   default: created_at
--   tiebreak: created_at DESC

-- name: List :many output=WorkorderRow paginate=workorder_sort
SELECT workorder_id, created_at
FROM p_workorder
WHERE status = ANY(pggen.arg('status'))
ORDER BY pggen.orderby('workorder_sort')
LIMIT pggen.arg('limit') OFFSET pggen.arg('offset');
`

func TestExpandPaginatedQueries_OrderingOnly(t *testing.T) {
	got := parseQueriesByName(t, orderingOnlySrc)

	wantNames := []string{"List__default", "List__created_at_desc", "List__created_at_asc", "List__execute_at_desc", "List__execute_at_asc"}
	for _, n := range wantNames {
		if _, ok := got[n]; !ok {
			t.Fatalf("missing variant %q; got %v", n, keys(got))
		}
	}

	// Expression key with internal commas survives intact, plus the tiebreak.
	execDesc := got["List__execute_at_desc"]
	wantOrder := "ORDER BY COALESCE(execute_at, activate_at, created_at) DESC, created_at DESC"
	if !strings.Contains(execDesc.PreparedSQL, wantOrder) {
		t.Errorf("missing %q in:\n%s", wantOrder, execDesc.PreparedSQL)
	}
	// Ordering-only: no keyset predicate, OFFSET preserved, filters intact.
	if strings.Contains(execDesc.PreparedSQL, "IS NOT DISTINCT FROM") {
		t.Errorf("ordering-only variant should not have a keyset predicate:\n%s", execDesc.PreparedSQL)
	}
	if !strings.Contains(execDesc.PreparedSQL, "OFFSET") {
		t.Errorf("OFFSET not preserved:\n%s", execDesc.PreparedSQL)
	}

	// Default variant: default column ASC + tiebreak, no NULLS coupling.
	def := got["List__default"]
	if !strings.Contains(def.PreparedSQL, "ORDER BY created_at ASC, created_at DESC") {
		t.Errorf("default ordering wrong:\n%s", def.PreparedSQL)
	}
}

func TestExpandPaginatedQueries_KeysetPlaceholderWithoutCursor(t *testing.T) {
	src := `
-- sort: bad_sort
--   key created_at: created_at
--   default: created_at

-- name: List :many output=Row paginate=bad_sort
SELECT 1 FROM t WHERE pggen.keyset('bad_sort') ORDER BY pggen.orderby('bad_sort');
`
	_, err := ParseFile(gotok.NewFileSet(), "test.sql", src, 0)
	if err == nil || !strings.Contains(err.Error(), "no cursor bindings") {
		t.Fatalf("want 'no cursor bindings' error, got %v", err)
	}
}

func TestExpandPaginatedQueries_RequiresOutputType(t *testing.T) {
	src := `
-- sort: s
--   key created_at: created_at
--   default: created_at

-- name: List :many paginate=s
SELECT 1 FROM t ORDER BY pggen.orderby('s');
`
	_, err := ParseFile(gotok.NewFileSet(), "test.sql", src, 0)
	if err == nil || !strings.Contains(err.Error(), "no output= pragma") {
		t.Fatalf("want 'no output= pragma' error, got %v", err)
	}
}

func TestExpandPaginatedQueries_UnknownSpec(t *testing.T) {
	src := `
-- name: List :many paginate=missing_spec
SELECT 1 WHERE pggen.keyset('missing_spec') ORDER BY pggen.orderby('missing_spec');
`
	_, err := ParseFile(gotok.NewFileSet(), "test.sql", src, 0)
	if err == nil || !strings.Contains(err.Error(), "unknown sort spec") {
		t.Fatalf("want unknown sort spec error, got %v", err)
	}
}

func keys(m map[string]*ast.SourceQuery) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
