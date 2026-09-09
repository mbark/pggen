package clickhouse_multi

import (
	"context"
	"testing"

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
	err := conn.Exec(ctx, `INSERT INTO call_record VALUES
		('46701234567', 'GPRS', 'tele2', 1024),
		('46701234567', 'GPRS', 'tele2', 2048),
		('46709999999', 'MOC',  'telia', 60)`)
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

	t.Run("the querier satisfies Querier", func(t *testing.T) {
		var _ Querier = q
	})
}
