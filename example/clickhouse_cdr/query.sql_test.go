package clickhouse_cdr

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/google/uuid"
	"github.com/mbark/pggen/internal/chtest"
	"github.com/mbark/pggen/internal/ptrs"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuerier(t *testing.T) {
	conn, _ := chtest.NewClickHouseDB(t, []string{"schema.sql"})
	q := NewQuerier(conn)
	ctx := context.Background()

	start := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	subID := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	custID := uuid.MustParse("22222222-2222-2222-2222-222222222222")

	insert := func(t *testing.T, aNum, provider string, units int64, at time.Time) {
		t.Helper()
		err := q.InsertCDR(ctx, InsertCDRParams{
			ANum:           aNum,
			RecordType:     "GPRS",
			Rat:            ptrs.String("LTE"),
			Provider:       provider,
			Units:          units,
			Charge:         decimal.RequireFromString("1.500000"),
			StartDate:      at,
			EndDate:        nil,
			UpdatedAt:      at,
			Labels:         []string{"a", "b"},
			Metadata:       map[string]string{"k": "v"},
			SubscriptionID: subID,
			CustomerID:     &custID,
			Premium:        false,
		})
		require.NoError(t, err, "InsertCDR")
	}

	insert(t, "46701234567", "tele2", 1024, start)
	insert(t, "46701234567", "tele2", 2048, start.Add(time.Hour))
	insert(t, "46709999999", "telia", 512, start)

	t.Run("FindDataUsage sums per subscriber", func(t *testing.T) {
		got, err := q.FindDataUsage(ctx, FindDataUsageParams{
			Msisdns: []string{"46701234567", "46709999999"},
			From:    start.Add(-time.Hour),
			To:      start.Add(24 * time.Hour),
		})
		require.NoError(t, err)
		require.Len(t, got, 2)

		assert.Equal(t, "46701234567", got[0].Msisdn)
		assert.Equal(t, int64(3072), got[0].DataBytes)
		assert.True(t, decimal.RequireFromString("3").Equal(got[0].TotalCharge),
			"summed charge should be 3, got %s", got[0].TotalCharge)
		assert.Equal(t, start.Add(time.Hour).UTC(), got[0].LastSeen.UTC())

		assert.Equal(t, "46709999999", got[1].Msisdn)
		assert.Equal(t, int64(512), got[1].DataBytes)
	})

	t.Run("FindDataUsage returns nothing outside the window", func(t *testing.T) {
		got, err := q.FindDataUsage(ctx, FindDataUsageParams{
			Msisdns: []string{"46701234567"},
			From:    start.Add(48 * time.Hour),
			To:      start.Add(72 * time.Hour),
		})
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	// Covers the whole type vocabulary in one row struct: an enum, a
	// LowCardinality(Nullable), a Decimal, a DateTime64, an array, a map, and
	// both a plain and a nullable UUID.
	t.Run("FindCDRsByProvider round trips every column type", func(t *testing.T) {
		got, err := q.FindCDRsByProvider(ctx, "telia")
		require.NoError(t, err)
		require.Len(t, got, 1)

		row := got[0]
		assert.Equal(t, "46709999999", row.ANum)
		// An Enum8 column comes back as its label; chgen maps it to string.
		assert.Equal(t, "GPRS", row.RecordType)
		require.NotNil(t, row.Rat)
		assert.Equal(t, "LTE", *row.Rat)
		assert.Equal(t, "telia", row.Provider)
		assert.Equal(t, int64(512), row.Units)
		assert.True(t, decimal.RequireFromString("1.5").Equal(row.Charge), "charge %s", row.Charge)
		assert.Equal(t, start.UTC(), row.StartDate.UTC())
		assert.Nil(t, row.EndDate, "end_date was inserted as NULL")
		assert.Equal(t, []string{"a", "b"}, row.Labels)
		assert.Equal(t, map[string]string{"k": "v"}, row.Metadata)
		assert.Equal(t, subID, row.SubscriptionID)
		require.NotNil(t, row.CustomerID)
		assert.Equal(t, custID, *row.CustomerID)
		assert.False(t, row.Premium)
	})

	t.Run("FindOldestStart returns a single value", func(t *testing.T) {
		got, err := q.FindOldestStart(ctx, "tele2")
		require.NoError(t, err)
		assert.Equal(t, start.UTC(), got.UTC())
	})

	// A single column over many rows can't use struct scanning, so this
	// exercises the other generated shape.
	t.Run("ListProviders scans a bare column", func(t *testing.T) {
		got, err := q.ListProviders(ctx)
		require.NoError(t, err)
		assert.Equal(t, []string{"tele2", "telia"}, got)
	})

	// A shared output= struct is emitted by a different code path than a
	// per-query row struct, and clickhouse-go binds columns to fields by ch
	// tag, so scanning into one is the only thing that proves it has them.
	t.Run("two queries share one row struct", func(t *testing.T) {
		byProvider, err := q.FindChargesByProvider(ctx, "tele2")
		require.NoError(t, err)
		require.Len(t, byProvider, 1)
		assert.Equal(t, "46701234567", byProvider[0].ANum)
		assert.True(t, decimal.RequireFromString("3").Equal(byProvider[0].Charge),
			"summed charge should be 3, got %s", byProvider[0].Charge)
		assert.Nil(t, byProvider[0].LastEnd, "every end_date was inserted as NULL")

		byLabel, err := q.FindChargesByLabel(ctx, "a")
		require.NoError(t, err)
		require.Len(t, byLabel, 2)

		// The same Go type backs both, which is the point of output=:
		// concatenating them only compiles if they share it.
		all := append(append([]ChargeRow{}, byProvider...), byLabel...)
		assert.Len(t, all, 3)
	})

	// limit and offset are ClickHouse setting names. Inference has to rename
	// them to describe the query; the generated code keeps the query's own
	// names because it binds client-side.
	t.Run("parameters named after ClickHouse settings", func(t *testing.T) {
		got, err := q.ListCDRsPaged(ctx, ListCDRsPagedParams{
			Provider: "tele2",
			Limit:    1,
			Offset:   1,
		})
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, int64(2048), got[0].Units, "OFFSET 1 should skip the first row")
	})

	t.Run("the querier satisfies Querier", func(t *testing.T) {
		var _ Querier = q
	})
}

// TestFindDataUsageSQL_MaterializedIntoATempTable uses the constant the sql=
// pragma exports.
//
// A generated method runs its query. This is the other thing a caller can want:
// the query as text, to put inside a statement pggen does not generate — here a
// CREATE TEMPORARY TABLE ... AS (…), which is how you materialize an expensive
// SELECT once and then read it back several times. Without the pragma the
// caller keeps a second copy of the SQL, and nothing checks the two agree.
func TestFindDataUsageSQL_MaterializedIntoATempTable(t *testing.T) {
	conn, _ := chtest.NewClickHouseDB(t, []string{"schema.sql"})
	q := NewQuerier(conn)
	ctx := context.Background()

	start := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	custID := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	require.NoError(t, q.InsertCDR(ctx, InsertCDRParams{
		ANum:           "46700000001",
		RecordType:     "GPRS",
		Provider:       "telia",
		Units:          4096,
		Charge:         decimal.RequireFromString("2.500000"),
		StartDate:      start,
		UpdatedAt:      start,
		Labels:         []string{},
		Metadata:       map[string]string{},
		SubscriptionID: uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		// Nullable(UUID) is a *uuid.UUID, and clickhouse-go v2.48.0 panics
		// calling Value on a nil one, so give it a value.
		CustomerID: &custID,
	}))

	// The constant still carries the query's parameters, so the statement that
	// wraps it binds them exactly as the generated method would.
	err := conn.Exec(ctx, "CREATE TEMPORARY TABLE usage AS ("+FindDataUsageSQL+")",
		clickhouse.Named("msisdns", []string{"46700000001"}),
		clickhouse.Named("from", start.Add(-time.Hour)),
		clickhouse.Named("to", start.Add(time.Hour)),
	)
	require.NoError(t, err, "materialize FindDataUsageSQL")

	var rows []FindDataUsageRow
	require.NoError(t, conn.Select(ctx, &rows, "SELECT * FROM usage"))
	require.Len(t, rows, 1)
	assert.Equal(t, "46700000001", rows[0].Msisdn)
	assert.Equal(t, int64(4096), rows[0].DataBytes)
}

// TestSumUnitsFrom_ReadsTheTableItIsGiven covers the {…:Identifier} parameter.
//
// A table name cannot be bound like a value, so the generated method puts it
// into the query text. Three things have to hold: the query reads whichever
// table it is handed, the ordinary parameter beside it is still bound — which
// is why pggen substitutes rather than letting ClickHouse bind the identifier
// — and a value that is not a plain identifier is refused rather than quoted.
func TestSumUnitsFrom_ReadsTheTableItIsGiven(t *testing.T) {
	conn, _ := chtest.NewClickHouseDB(t, []string{"schema.sql"})
	q := NewQuerier(conn)
	ctx := context.Background()

	start := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	row := func(table, provider string, units int64, at time.Time) {
		t.Helper()
		require.NoError(t, conn.Exec(ctx, "INSERT INTO "+table+
			" (a_num, record_type, provider, units, charge, start_date, updated_at, subscription_id)"+
			" VALUES ('46700000002', 'GPRS', ?, ?, 0, ?, ?, '11111111-1111-1111-1111-111111111111')",
			provider, units, at, at))
	}
	row("cdr", "telia", 10, start)
	row("cdr_archive", "telia", 99, start)

	t.Run("the live table", func(t *testing.T) {
		got, err := q.SumUnitsFrom(ctx, "cdr", start.Add(-time.Hour))
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, int64(10), got[0].Units)
	})

	t.Run("the archive table, same query", func(t *testing.T) {
		got, err := q.SumUnitsFrom(ctx, "cdr_archive", start.Add(-time.Hour))
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, int64(99), got[0].Units)
	})

	// The identifier travels as text, so the bound parameter next to it has to
	// keep working. A from after the record leaves nothing.
	t.Run("the bound parameter still filters", func(t *testing.T) {
		got, err := q.SumUnitsFrom(ctx, "cdr", start.Add(time.Hour))
		require.NoError(t, err)
		assert.Empty(t, got)
	})

	t.Run("a value that is not a plain identifier is refused", func(t *testing.T) {
		for _, bad := range []string{
			"cdr WHERE 1=1 --",
			"cdr; DROP TABLE cdr",
			"cdr`",
			"",
			"1cdr",
		} {
			_, err := q.SumUnitsFrom(ctx, bad, start)
			require.Error(t, err, "SumUnitsFrom(%q)", bad)
			assert.NotContains(t, err.Error(), "Unknown table",
				"%q must be refused before it reaches the server", bad)
		}
	})

	t.Run("a database-qualified name is allowed", func(t *testing.T) {
		var db string
		require.NoError(t, conn.QueryRow(ctx, "SELECT currentDatabase()").Scan(&db))

		got, err := q.SumUnitsFrom(ctx, db+".cdr", start.Add(-time.Hour))
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, int64(10), got[0].Units)
	})
}

// TestPageCDRs covers the paginate= pragma on ClickHouse.
//
// The pragma fans one query out into a statement per sort key and direction,
// each with a clean ORDER BY and a matching cursor predicate, behind a
// dispatcher. What has to hold is that the first page — every cursor argument
// nil — returns the start of the ordering rather than nothing, that a cursor
// taken from the last row of a page returns the next page and never repeats a
// row, and that the two directions are mirror images.
//
// The nil first page is the part that would fail quietly: ClickHouse rejects
// NULL for a non-nullable parameter, which is why a cursor binding must be
// declared Nullable.
func TestPageCDRs(t *testing.T) {
	conn, _ := chtest.NewClickHouseDB(t, []string{"schema.sql"})
	q := NewQuerier(conn)
	ctx := context.Background()

	start := time.Date(2026, 5, 1, 8, 0, 0, 0, time.UTC)
	for i, units := range []int64{30, 10, 20} {
		require.NoError(t, conn.Exec(ctx,
			"INSERT INTO cdr (a_num, record_type, provider, units, charge, start_date, updated_at, subscription_id)"+
				" VALUES (?, 'GPRS', 'telia', ?, 0, ?, ?, '11111111-1111-1111-1111-111111111111')",
			fmt.Sprintf("4670000000%d", i), units,
			start.Add(time.Duration(i)*time.Hour), start))
	}

	// A first page asks with no cursor at all, which is every cursor argument
	// nil.
	firstPage := func(t *testing.T, sortKey string, desc bool, limit uint32) []CDRPageRow {
		t.Helper()
		got, err := q.PageCDRs(ctx, PageCDRsParams{
			Provider: "telia", Limit: limit,
			SortKey: sortKey, Descending: desc,
		})
		require.NoError(t, err)
		return got
	}

	t.Run("the default sort pages through every row", func(t *testing.T) {
		page := firstPage(t, "", false, 2)
		require.Len(t, page, 2)
		assert.Equal(t, "46700000000", page[0].ANum)
		assert.Equal(t, "46700000001", page[1].ANum)

		next, err := q.PageCDRs(ctx, PageCDRsParams{
			Provider: "telia", Limit: 2,
			AfterANum: &page[1].ANum,
		})
		require.NoError(t, err)
		require.Len(t, next, 1, "the last row, and none of the first page again")
		assert.Equal(t, "46700000002", next[0].ANum)
	})

	t.Run("sorting by units ascending", func(t *testing.T) {
		page := firstPage(t, PageCDRsSortUnits, false, 10)
		require.Len(t, page, 3)
		assert.Equal(t, []int64{10, 20, 30},
			[]int64{page[0].Units, page[1].Units, page[2].Units})
	})

	t.Run("sorting by units descending is the mirror", func(t *testing.T) {
		page := firstPage(t, PageCDRsSortUnits, true, 10)
		require.Len(t, page, 3)
		assert.Equal(t, []int64{30, 20, 10},
			[]int64{page[0].Units, page[1].Units, page[2].Units})
	})

	t.Run("a cursor on the units sort takes the next page", func(t *testing.T) {
		page := firstPage(t, PageCDRsSortUnits, false, 1)
		require.Len(t, page, 1)
		assert.Equal(t, int64(10), page[0].Units)

		next, err := q.PageCDRs(ctx, PageCDRsParams{
			Provider: "telia", Limit: 1,
			SortKey:    PageCDRsSortUnits,
			AfterUnits: &page[0].Units,
			AfterANum:  &page[0].ANum,
		})
		require.NoError(t, err)
		require.Len(t, next, 1)
		assert.Equal(t, int64(20), next[0].Units)
	})

	t.Run("sorting by start_date descending", func(t *testing.T) {
		page := firstPage(t, PageCDRsSortStartDate, true, 10)
		require.Len(t, page, 3)
		assert.Equal(t, start.Add(2*time.Hour), page[0].StartDate)
		assert.Equal(t, start, page[2].StartDate)
	})

	t.Run("an unknown sort key is an error", func(t *testing.T) {
		_, err := q.PageCDRs(ctx, PageCDRsParams{
			Provider: "telia", Limit: 10, SortKey: "nope",
		})
		require.ErrorContains(t, err, "unknown sort key")
	})
}
