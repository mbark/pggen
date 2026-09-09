// Package chtest creates isolated ClickHouse databases for tests, mirroring
// internal/pgtest.
//
// ClickHouse has no search_path, so where pgtest isolates a test in a schema
// on a shared database, chtest isolates it in a database of its own on the
// shared server.
package chtest

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"

	"github.com/mbark/pggen/internal/chdocker"
)

// CleanupFunc drops the database and closes the connections.
type CleanupFunc func()

// Addr is the local ClickHouse from docker-compose.yml. The native port is
// 9010 rather than 9000 so it can coexist with another project's ClickHouse.
const (
	Addr     = "127.0.0.1:9010"
	User     = "default"
	Password = "hunter2"
)

// Options returns the connection options for the local test ClickHouse,
// pointed at database.
func Options(database string) *clickhouse.Options {
	return &clickhouse.Options{
		Addr: []string{Addr},
		Auth: clickhouse.Auth{Database: database, Username: User, Password: Password},
	}
}

// DSN returns the connection string for the local test ClickHouse, pointed at
// database.
func DSN(database string) string {
	return fmt.Sprintf("clickhouse://%s:%s@%s/%s", User, Password, Addr, database)
}

// NewClickHouseDBString opens a connection to a randomly named, new database
// and runs the statements in sql against it. Statements are separated by
// semicolons: ClickHouse has no multi-statement Exec. It also returns the DSN
// for that database, for tests that drive pggen through its public API.
func NewClickHouseDBString(t *testing.T, sql string) (driver.Conn, string, CleanupFunc) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := clickhouse.Open(Options("default"))
	if err != nil {
		t.Fatalf("connect to docker clickhouse: %s", err)
	}
	if err := admin.Ping(ctx); err != nil {
		t.Fatalf("ping docker clickhouse at %s: %s", Addr, err)
	}

	database := "pggen_test_" + strconv.Itoa(int(rand.Int31()))
	if err := admin.Exec(ctx, "CREATE DATABASE "+database); err != nil {
		t.Fatalf("create new database: %s", err)
	}
	t.Logf("created database: %s", database)

	conn, err := clickhouse.Open(Options(database))
	if err != nil {
		t.Fatalf("connect to new database %s: %s", database, err)
	}
	for _, stmt := range chdocker.SplitStatements(sql) {
		if err := conn.Exec(ctx, stmt); err != nil {
			t.Fatalf("run sql %q: %s", truncate(stmt), err)
		}
	}

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := conn.Close(); err != nil {
			t.Errorf("close database conn: %s", err)
		}
		if err := admin.Exec(ctx, "DROP DATABASE "+database); err != nil {
			t.Errorf("drop database %s: %s", database, err)
		}
		if err := admin.Close(); err != nil {
			t.Errorf("close admin conn: %s", err)
		}
	}
	return conn, DSN(database), cleanup
}

// NewClickHouseDB opens a connection to a randomly named, new database and
// runs all sqlFiles against it.
func NewClickHouseDB(t *testing.T, sqlFiles []string) (driver.Conn, string, CleanupFunc) {
	t.Helper()
	sb := &strings.Builder{}
	for _, file := range sqlFiles {
		bs, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read test db sql file: %s", err)
		}
		sb.Write(bs)
		sb.WriteString(";\n")
	}
	return NewClickHouseDBString(t, sb.String())
}

func truncate(s string) string {
	const max = 120
	if len(s) <= max {
		return s
	}
	return fmt.Sprintf("%s… (%d bytes)", s[:max], len(s))
}
