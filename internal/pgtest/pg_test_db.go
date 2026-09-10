// Package pgtest creates isolated Postgres schemas for tests.
//
// The Postgres behind these tests is the long-lived one from docker-compose,
// so anything created here and then abandoned stays there until someone drops
// it by hand. Each resource is handed to t.Cleanup as soon as it exists, which
// releases it in reverse order and runs even for the t.Fatalf calls below.
package pgtest

import (
	"context"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/mbark/pggen/internal/errs"
)

type Option func(config *pgx.ConnConfig)

// NewPostgresSchemaString opens a connection with search_path set to a randomly
// named, new schema and loads the sql string.
func NewPostgresSchemaString(t *testing.T, sql string, opts ...Option) *pgx.Conn {
	t.Helper()
	// Create a new schema.
	connStr := "user=postgres password=hunter2 host=localhost port=5555 dbname=pggen"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		t.Fatalf("connect to docker postgres: %s", err)
	}
	// Registered first so it runs last: t.Cleanup is LIFO, and dropping the
	// schema below needs this connection.
	t.Cleanup(func() { errs.CaptureT(t, closeConn(conn), "close admin conn") })

	schema := "pggen_test_" + strconv.Itoa(int(rand.Int31()))
	if _, err = conn.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create new schema: %s", err)
	}
	t.Logf("created schema: %s", schema)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		errs.CaptureT(t, func() error {
			_, err := conn.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE")
			return err
		}, "drop schema "+schema)
	})

	// Load SQL files into new schema.
	connStr += " search_path=" + schema + ",public"
	connCfg, err := pgx.ParseConfig(connStr)
	if err != nil {
		t.Fatalf("parse config: %q: %s", connStr, err)
	}
	for _, opt := range opts {
		opt(connCfg)
	}
	schemaConn, err := pgx.ConnectConfig(ctx, connCfg)
	if err != nil {
		t.Fatalf("connect to docker postgres with search path: %s", err)
	}
	t.Cleanup(func() { errs.CaptureT(t, closeConn(schemaConn), "close schema conn") })

	if _, err := schemaConn.Exec(ctx, sql); err != nil {
		t.Fatalf("run sql: %s", err)
	}
	return schemaConn
}

// NewPostgresSchema opens a connection with search_path set to a randomly
// named, new schema and loads all sqlFiles.
func NewPostgresSchema(t *testing.T, sqlFiles []string, opts ...Option) *pgx.Conn {
	t.Helper()
	sb := &strings.Builder{}
	for _, file := range sqlFiles {
		bs, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read test db sql file: %s", err)
		}
		sb.Write(bs)
		sb.WriteString(";\n\n -- FILE: ")
		sb.WriteString(file)
		sb.WriteString("\n")

	}
	return NewPostgresSchemaString(t, sb.String(), opts...)
}

// closeConn adapts pgx's context-taking Close to the func() error that
// errs.CaptureT wants. It uses a context of its own: by cleanup time the one
// the connection was opened with is long cancelled.
func closeConn(conn *pgx.Conn) func() error {
	return func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return conn.Close(ctx)
	}
}
