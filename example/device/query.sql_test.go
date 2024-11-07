package device

import (
	"context"
	"github.com/jackc/pgtype"
	"github.com/jackc/pgx/v4"
	"github.com/mbark/pggen/internal/pgtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"net"
	"testing"
)

func TestQuerier_FindDevicesByUser(t *testing.T) {
	conn, cleanup := pgtest.NewPostgresSchema(t, []string{"schema.sql"})
	defer cleanup()
	q := NewQuerier(conn)
	ctx := context.Background()
	userID := 18
	_, err := q.InsertUser(ctx, userID, "foo")
	require.NoError(t, err)
	mac1, _ := net.ParseMAC("11:22:33:44:55:66")
	_, err = q.InsertDevice(ctx, pgtype.Macaddr{Status: pgtype.Present, Addr: mac1}, userID)
	require.NoError(t, err)

	t.Run("FindDevicesByUser", func(t *testing.T) {
		val, err := q.FindDevicesByUser(ctx, userID)
		require.NoError(t, err)
		want := []FindDevicesByUserRow{
			{
				ID:   userID,
				Name: "foo",
				MacAddrs: pgtype.MacaddrArray{
					Elements: []pgtype.Macaddr{{Addr: mac1, Status: pgtype.Present}},
					Dimensions: []pgtype.ArrayDimension{{
						Length:     1,
						LowerBound: 1,
					}},
					Status: pgtype.Present,
				},
			},
		}
		assert.Equal(t, want, val)
	})

	t.Run("FindDevicesByUserBatch", func(t *testing.T) {
		batch := &pgx.Batch{}
		q.FindDevicesByUserBatch(batch, userID)
		results := conn.SendBatch(ctx, batch)
		got, err := q.FindDevicesByUserScan(results)
		require.NoError(t, err)
		want := []FindDevicesByUserRow{
			{
				ID:   userID,
				Name: "foo",
				MacAddrs: pgtype.MacaddrArray{
					Elements: []pgtype.Macaddr{{Addr: mac1, Status: pgtype.Present}},
					Dimensions: []pgtype.ArrayDimension{{
						Length:     1,
						LowerBound: 1,
					}},
					Status: pgtype.Present,
				},
			},
		}
		assert.Equal(t, want, got)
	})
}

func TestQuerier_CompositeUser(t *testing.T) {
	conn, cleanup := pgtest.NewPostgresSchema(t, []string{"schema.sql"})
	defer cleanup()
	q := NewQuerier(conn)
	ctx := context.Background()

	userID := 18
	name := "foo"
	_, err := q.InsertUser(ctx, userID, name)
	require.NoError(t, err)

	mac1, _ := net.ParseMAC("11:22:33:44:55:66")
	mac2, _ := net.ParseMAC("aa:bb:cc:dd:ee:ff")
	_, err = q.InsertDevice(ctx, pgtype.Macaddr{Status: pgtype.Present, Addr: mac1}, userID)
	require.NoError(t, err)
	_, err = q.InsertDevice(ctx, pgtype.Macaddr{Status: pgtype.Present, Addr: mac2}, userID)
	require.NoError(t, err)

	t.Run("CompositeUser", func(t *testing.T) {
		users, err := q.CompositeUser(ctx)
		require.NoError(t, err)
		want := []CompositeUserRow{
			{
				Mac:  pgtype.Macaddr{Addr: mac1, Status: pgtype.Present},
				Type: DeviceTypeUndefined,
				User: User{ID: &userID, Name: &name},
			},
			{
				Mac:  pgtype.Macaddr{Addr: mac2, Status: pgtype.Present},
				Type: DeviceTypeUndefined,
				User: User{ID: &userID, Name: &name},
			},
		}
		assert.Equal(t, want, users)
	})

	t.Run("CompositeUserBatch", func(t *testing.T) {
		batch := &pgx.Batch{}
		q.CompositeUserBatch(batch)
		results := conn.SendBatch(ctx, batch)
		got, err := q.CompositeUserScan(results)
		want := []CompositeUserRow{
			{
				Mac:  pgtype.Macaddr{Addr: mac1, Status: pgtype.Present},
				Type: DeviceTypeUndefined,
				User: User{ID: &userID, Name: &name},
			},
			{
				Mac:  pgtype.Macaddr{Addr: mac2, Status: pgtype.Present},
				Type: DeviceTypeUndefined,
				User: User{ID: &userID, Name: &name},
			},
		}
		require.NoError(t, err)
		assert.Equal(t, want, got)
	})

}

func TestQuerier_CompositeUserOne(t *testing.T) {
	conn, cleanup := pgtest.NewPostgresSchema(t, []string{"schema.sql"})
	defer cleanup()
	q := NewQuerier(conn)
	ctx := context.Background()
	id := 15
	name := "qux"
	wantUser := User{ID: &id, Name: &name}

	t.Run("CompositeUserOne", func(t *testing.T) {
		got, err := q.CompositeUserOne(ctx)
		require.NoError(t, err)
		assert.Equal(t, wantUser, got)
	})

	t.Run("CompositeUserOneBatch", func(t *testing.T) {
		gotUserTwoCols, err := q.CompositeUserOneTwoCols(ctx)
		require.NoError(t, err)
		assert.Equal(t, CompositeUserOneTwoColsRow{
			Num:  1,
			User: wantUser,
		}, gotUserTwoCols)

		batch := &pgx.Batch{}
		q.CompositeUserOneBatch(batch)
		q.CompositeUserOneTwoColsBatch(batch)
		results := conn.SendBatch(ctx, batch)

		gotOneScan, err := q.CompositeUserOneScan(results)
		require.NoError(t, err)
		assert.Equal(t, wantUser, gotOneScan)

		gotTwoScan, err := q.CompositeUserOneTwoColsScan(results)
		require.NoError(t, err)
		assert.Equal(t, CompositeUserOneTwoColsRow{
			Num:  1,
			User: wantUser,
		}, gotTwoScan)
	})
}
