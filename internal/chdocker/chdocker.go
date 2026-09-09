// Package chdocker creates one-off ClickHouse docker containers so pggen can
// introspect a schema. The generic container plumbing lives in
// internal/dbdocker; this supplies the ClickHouse-specific parts.
package chdocker

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"text/template"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"

	"github.com/mbark/pggen/internal/dbdocker"
	"github.com/mbark/pggen/internal/ports"
)

// The official image has no equivalent of POSTGRES_HOST_AUTH_METHOD=trust: it
// requires a password unless user setup is skipped outright. These are for a
// throwaway container that only ever listens on localhost.
const (
	user     = "default"
	password = "pggen"
	database = "pggen"
)

// Client controls a running ClickHouse Docker container.
type Client struct {
	db         *dbdocker.Client
	connString string
}

// Start builds a Docker image with the schema files as init scripts and runs
// it in a container.
func Start(ctx context.Context, initScripts []string) (*Client, error) {
	dockerfile, err := renderDockerfile(initScripts)
	if err != nil {
		return nil, err
	}
	db, err := dbdocker.Start(ctx, dbdocker.Config{
		Name:        "clickhouse",
		Dockerfile:  dockerfile,
		InitScripts: initScripts,
		Env: []string{
			"CLICKHOUSE_DB=" + database,
			"CLICKHOUSE_USER=" + user,
			"CLICKHOUSE_PASSWORD=" + password,
			"CLICKHOUSE_DEFAULT_ACCESS_MANAGEMENT=1",
		},
		Port: "9000/tcp",
		// Keep the data off disk; the container is thrown away either way.
		Tmpfs:     map[string]string{"/var/lib/clickhouse": ""},
		WaitReady: waitIsReady,
	})
	if err != nil {
		return nil, err
	}
	return &Client{db: db, connString: connString(db.Port())}, nil
}

func connString(port ports.Port) string {
	return fmt.Sprintf("clickhouse://%s:%s@127.0.0.1:%d/%s", user, password, port, database)
}

func renderDockerfile(initScripts []string) (string, error) {
	buf := &bytes.Buffer{}
	tmpl, err := template.New("chdocker").Parse(dockerfileTemplate)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	err = tmpl.ExecuteTemplate(buf, "dockerfile", chTemplate{
		InitScripts: dbdocker.InitScriptNames(initScripts),
	})
	if err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// waitIsReady waits until ClickHouse accepts a query.
//
// It waits for a query rather than a connection: the server accepts
// connections before the init scripts have finished, and querying system.one
// is the cheapest way to know the database is actually usable.
func waitIsReady(ctx context.Context, port ports.Port) error {
	opts, err := clickhouse.ParseDSN(connString(port))
	if err != nil {
		return fmt.Errorf("parse conn string: %w", err)
	}
	opts.DialTimeout = time.Second

	deadline := time.After(60 * time.Second)
	for {
		select {
		case <-deadline:
			return fmt.Errorf("clickhouse didn't start up within 60 seconds")
		case <-ctx.Done():
			return fmt.Errorf("clickhouse didn't start up before context expired")
		default:
			// continue
		}
		debounce := time.After(300 * time.Millisecond)
		conn, err := clickhouse.Open(opts)
		if err == nil {
			err = conn.Exec(ctx, "SELECT 1")
			if closeErr := conn.Close(); closeErr != nil {
				slog.DebugContext(ctx, "close clickhouse connection", slog.String("error", closeErr.Error()))
			}
			if err == nil {
				return nil
			}
		}
		slog.DebugContext(ctx, "attempted connection", slog.String("error", err.Error()))
		<-debounce
	}
}

// ConnString returns the DSN for the started ClickHouse container.
func (c *Client) ConnString() string { return c.connString }

// GetContainerLogs returns all stderr and stdout logs for the container.
func (c *Client) GetContainerLogs() (string, error) { return c.db.GetContainerLogs() }

// Stop stops the running container, if any.
func (c *Client) Stop(ctx context.Context) error { return c.db.Stop(ctx) }

// SplitStatements splits a SQL string on semicolons that are not inside a
// string literal or a comment. ClickHouse executes one statement per call, so
// a schema file has to be taken apart before it can be loaded.
func SplitStatements(sql string) []string {
	var stmts []string
	var sb strings.Builder
	for i := 0; i < len(sql); i++ {
		c := sql[i]
		switch c {
		case '\'', '`', '"':
			quote := c
			sb.WriteByte(c)
			for i++; i < len(sql); i++ {
				sb.WriteByte(sql[i])
				if sql[i] == '\\' && i+1 < len(sql) {
					i++
					sb.WriteByte(sql[i])
					continue
				}
				if sql[i] == quote {
					break
				}
			}
		case '-':
			if i+1 < len(sql) && sql[i+1] == '-' {
				for i < len(sql) && sql[i] != '\n' {
					i++
				}
				sb.WriteByte('\n')
			} else {
				sb.WriteByte(c)
			}
		case ';':
			if stmt := strings.TrimSpace(sb.String()); stmt != "" {
				stmts = append(stmts, stmt)
			}
			sb.Reset()
		default:
			sb.WriteByte(c)
		}
	}
	if stmt := strings.TrimSpace(sb.String()); stmt != "" {
		stmts = append(stmts, stmt)
	}
	return stmts
}
