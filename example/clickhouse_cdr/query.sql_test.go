package clickhouse_cdr

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mbark/pggen/internal/chtest"
	"github.com/mbark/pggen/internal/ptrs"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuerier(t *testing.T) {
	conn, _, cleanup := chtest.NewClickHouseDB(t, []string{"schema.sql"})
	defer cleanup()
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
