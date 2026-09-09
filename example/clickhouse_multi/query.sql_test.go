package clickhouse_multi

import (
	"context"
	"testing"
	"time"

	"github.com/mbark/pggen/internal/chtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuerier(t *testing.T) {
	conn, _, cleanup := chtest.NewClickHouseDB(t, []string{"schema.sql"})
	defer cleanup()
	q := NewQuerier(conn)
	ctx := context.Background()

	// The package has no :exec query — every one of its queries reads — which
	// is the point: its genericConn asks for Select and nothing else. So the
	// fixtures go in through the connection directly.
	err := conn.Exec(ctx, `INSERT INTO call_record
		(msisdn_a, record_type, provider, units, ended_at, tag_sets) VALUES
		('46701234567', 'GPRS', 'tele2', 1024, NULL, [{'net': 'lte'}]),
		('46701234567', 'GPRS', 'tele2', 2048, '2026-01-01 10:00:00', []),
		('46709999999', 'MOC',  'telia', 60,   NULL, [])`)
	require.NoError(t, err)

	t.Run("SumByProvider", func(t *testing.T) {
		got, err := q.SumByProvider(ctx, "tele2")
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, "46701234567", got[0].MSISDNA)
		assert.Equal(t, int64(3072), got[0].Units)
		// --go-type mapped the enum column to the package's own RecordType.
		assert.Equal(t, "GPRS", got[0].RecordType.String())
	})

	// A query in the other file of the same package, reached through the same
	// DBQuerier and returning the same row struct.
	t.Run("SumByMSISDN comes from the other file", func(t *testing.T) {
		got, err := q.SumByMSISDN(ctx, "46709999999")
		require.NoError(t, err)
		require.Len(t, got, 1)
		assert.Equal(t, int64(60), got[0].Units)
		assert.Equal(t, "MOC", got[0].RecordType.String())
	})

	t.Run("the two files share one row struct", func(t *testing.T) {
		byProvider, err := q.SumByProvider(ctx, "telia")
		require.NoError(t, err)
		byMSISDN, err := q.SumByMSISDN(ctx, "46709999999")
		require.NoError(t, err)

		// Concatenating them only compiles if both are []UsageRow, which is
		// what output= across two files is for.
		all := append(append([]UsageRow{}, byProvider...), byMSISDN...)
		assert.Len(t, all, 2)
	})

	t.Run("SumBusySubscribers", func(t *testing.T) {
		got, err := q.SumBusySubscribers(ctx, 1000)
		require.NoError(t, err)
		require.Len(t, got, 1, "only the tele2 subscriber is over the threshold")
		assert.Equal(t, "46701234567", got[0].MSISDNA)
	})

	// The Go types of these two columns are built out of wrappers, and a
	// wrapper carries no import of its own: a file that reached "time" only
	// through a *time.Time did not import it, and a map inside an array
	// panicked while being qualified.
	t.Run("a pointer and an array of maps round trip", func(t *testing.T) {
		got, err := q.FindEndsByMSISDN(ctx, "46701234567")
		require.NoError(t, err)
		require.Len(t, got, 2)

		assert.Nil(t, got[0].EndedAt, "ended_at was inserted as NULL")
		assert.Equal(t, []map[string]string{{"net": "lte"}}, got[0].TagSets)

		require.NotNil(t, got[1].EndedAt)
		assert.Equal(t,
			time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
			got[1].EndedAt.UTC())
		assert.Empty(t, got[1].TagSets)
	})

	t.Run("the querier satisfies Querier", func(t *testing.T) {
		var _ Querier = q
	})
}
