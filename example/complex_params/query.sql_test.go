package complex_params

import (
	"context"
	"github.com/jackc/pgx/v4"
	"github.com/mbark/pggen/internal/pgtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestNewQuerier_ParamArrayInt(t *testing.T) {
	conn, cleanup := pgtest.NewPostgresSchema(t, []string{"schema.sql"})
	defer cleanup()

	q := NewQuerier(conn)
	ctx := context.Background()

	want := []int{1, 2, 3, 4}

	t.Run("ParamArrayInt", func(t *testing.T) {
		row, err := q.ParamArrayInt(ctx, want)
		require.NoError(t, err)
		assert.Equal(t, want, row)
	})

	t.Run("ParamArrayIntBatch", func(t *testing.T) {
		batch := &pgx.Batch{}
		q.ParamArrayIntBatch(batch, want)
		results := conn.SendBatch(ctx, batch)
		row, err := q.ParamArrayIntScan(results)
		if err != nil {
			t.Fatal(err)
		}
		assert.Equal(t, want, row)
	})
}

func TestNewQuerier_ParamNested1(t *testing.T) {
	conn, cleanup := pgtest.NewPostgresSchema(t, []string{"schema.sql"})
	defer cleanup()

	q := NewQuerier(conn)
	ctx := context.Background()

	want := Dimensions{Width: 77, Height: 77}

	t.Run("ParamNested1", func(t *testing.T) {
		row, err := q.ParamNested1(ctx, want)
		require.NoError(t, err)
		assert.Equal(t, want, row)
	})

	t.Run("ParamNested1Batch", func(t *testing.T) {
		batch := &pgx.Batch{}
		q.ParamNested1Batch(batch, want)
		results := conn.SendBatch(ctx, batch)
		row, err := q.ParamNested1Scan(results)
		if err != nil {
			t.Fatal(err)
		}
		assert.Equal(t, want, row)
	})
}

func TestNewQuerier_ParamNested2(t *testing.T) {
	conn, cleanup := pgtest.NewPostgresSchema(t, []string{"schema.sql"})
	defer cleanup()

	q := NewQuerier(conn)
	ctx := context.Background()

	want := ProductImageType{
		Source:     "src",
		Dimensions: Dimensions{Width: 77, Height: 77},
	}

	t.Run("ParamNested2", func(t *testing.T) {
		row, err := q.ParamNested2(ctx, want)
		require.NoError(t, err)
		assert.Equal(t, want, row)
	})

	t.Run("ParamNested2Batch", func(t *testing.T) {
		batch := &pgx.Batch{}
		q.ParamNested2Batch(batch, want)
		results := conn.SendBatch(ctx, batch)
		row, err := q.ParamNested2Scan(results)
		if err != nil {
			t.Fatal(err)
		}
		assert.Equal(t, want, row)
	})
}

func TestNewQuerier_ParamNested2Array(t *testing.T) {
	conn, cleanup := pgtest.NewPostgresSchema(t, []string{"schema.sql"})
	defer cleanup()

	q := NewQuerier(conn)
	ctx := context.Background()

	want := []ProductImageType{
		{Source: "src1", Dimensions: Dimensions{Width: 11, Height: 11}},
		{Source: "src2", Dimensions: Dimensions{Width: 22, Height: 22}},
	}

	t.Run("ParamNested2Array", func(t *testing.T) {
		row, err := q.ParamNested2Array(ctx, want)
		require.NoError(t, err)
		assert.Equal(t, want, row)
	})

	t.Run("ParamNested2ArrayBatch", func(t *testing.T) {
		batch := &pgx.Batch{}
		q.ParamNested2ArrayBatch(batch, want)
		results := conn.SendBatch(ctx, batch)
		row, err := q.ParamNested2ArrayScan(results)
		if err != nil {
			t.Fatal(err)
		}
		assert.Equal(t, want, row)
	})
}

func TestNewQuerier_ParamNested3(t *testing.T) {
	conn, cleanup := pgtest.NewPostgresSchema(t, []string{"schema.sql"})
	defer cleanup()

	q := NewQuerier(conn)
	ctx := context.Background()

	want := ProductImageSetType{
		Name:      "set1",
		OrigImage: ProductImageType{Source: "src1", Dimensions: Dimensions{Width: 11, Height: 11}},
		Images: []ProductImageType{
			{Source: "src1", Dimensions: Dimensions{Width: 11, Height: 11}},
			{Source: "src2", Dimensions: Dimensions{Width: 22, Height: 22}},
		},
	}

	t.Run("ParamNested3", func(t *testing.T) {
		row, err := q.ParamNested3(ctx, want)
		require.NoError(t, err)
		assert.Equal(t, want, row)
	})

	t.Run("ParamNested3Batch", func(t *testing.T) {
		batch := &pgx.Batch{}
		q.ParamNested3Batch(batch, want)
		results := conn.SendBatch(ctx, batch)
		row, err := q.ParamNested3Scan(results)
		if err != nil {
			t.Fatal(err)
		}
		assert.Equal(t, want, row)
	})
}

func TestNewQuerier_ParamNested3_QueryAllDataTypes(t *testing.T) {
	conn, cleanup := pgtest.NewPostgresSchema(t, []string{"schema.sql"})
	defer cleanup()
	ctx := context.Background()
	// dataTypes, err := QueryAllDataTypes(ctx, conn)
	// require.NoError(t, err)
	q := NewQuerier(conn)

	want := ProductImageSetType{
		Name:      "set1",
		OrigImage: ProductImageType{Source: "src1", Dimensions: Dimensions{Width: 11, Height: 11}},
		Images: []ProductImageType{
			{Source: "src1", Dimensions: Dimensions{Width: 11, Height: 11}},
			{Source: "src2", Dimensions: Dimensions{Width: 22, Height: 22}},
		},
	}

	t.Run("ParamNested3", func(t *testing.T) {
		row, err := q.ParamNested3(ctx, want)
		require.NoError(t, err)
		assert.Equal(t, want, row)
	})

	t.Run("ParamNested3Batch", func(t *testing.T) {
		batch := &pgx.Batch{}
		q.ParamNested3Batch(batch, want)
		results := conn.SendBatch(ctx, batch)
		row, err := q.ParamNested3Scan(results)
		if err != nil {
			t.Fatal(err)
		}
		assert.Equal(t, want, row)
	})
}
