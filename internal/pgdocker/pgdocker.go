// Package pgdocker creates one-off Postgres docker images to use so pggen can
// introspect the schema. The generic container plumbing lives in
// internal/dbdocker; this supplies the Postgres-specific parts.
package pgdocker

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"text/template"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mbark/pggen/internal/dbdocker"
	"github.com/mbark/pggen/internal/ports"
)

// Client is a client to control the running Postgres Docker container.
type Client struct {
	db         *dbdocker.Client
	connString string
}

// Start builds a Docker image and runs the image in a container.
func Start(ctx context.Context, initScripts []string) (*Client, error) {
	dockerfile, err := renderDockerfile(initScripts)
	if err != nil {
		return nil, err
	}
	db, err := dbdocker.Start(ctx, dbdocker.Config{
		Name:        "postgres",
		Dockerfile:  dockerfile,
		InitScripts: initScripts,
		Env:         []string{"POSTGRES_HOST_AUTH_METHOD=trust"},
		Cmd:         []string{"postgres", "-c", "fsync=off", "-c", "full_page_writes=off"},
		Port:        "5432/tcp",
		Tmpfs:       map[string]string{"/var/lib/postgresql/data": ""},
		WaitReady:   waitIsReady,
	})
	if err != nil {
		return nil, err
	}
	return &Client{db: db, connString: connString(db.Port())}, nil
}

func connString(port ports.Port) string {
	return fmt.Sprintf("host=0.0.0.0 port=%d user=postgres", port)
}

func renderDockerfile(initScripts []string) (string, error) {
	buf := &bytes.Buffer{}
	tmpl, err := template.New("pgdocker").Parse(dockerfileTemplate)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	err = tmpl.ExecuteTemplate(buf, "dockerfile", pgTemplate{
		InitScripts: dbdocker.InitScriptNames(initScripts),
	})
	if err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// waitIsReady waits until we can connect to the database.
func waitIsReady(ctx context.Context, port ports.Port) error {
	cfg, err := pgx.ParseConfig(connString(port) + " connect_timeout=1")
	if err != nil {
		return fmt.Errorf("parse conn string: %w", err)
	}

	deadline := time.After(10 * time.Second)
	for {
		select {
		case <-deadline:
			return fmt.Errorf("postgres didn't start up with 10 seconds")
		case <-ctx.Done():
			return fmt.Errorf("postgres didn't start up before context expired")
		default:
			// continue
		}
		debounce := time.After(200 * time.Millisecond)
		conn, err := pgx.ConnectConfig(ctx, cfg)
		if err == nil {
			if err := conn.Close(ctx); err != nil {
				slog.DebugContext(ctx, "close postgres connection", slog.String("error", err.Error()))
			}
			return nil
		}
		slog.DebugContext(ctx, "attempted connection", slog.String("error", err.Error()))
		<-debounce
	}
}

// GetContainerLogs returns a string of all stderr and stdout logs for a
// container.
func (c *Client) GetContainerLogs() (string, error) { return c.db.GetContainerLogs() }

// ConnString returns the connection string to connect to the started Postgres
// Docker container.
func (c *Client) ConnString() (string, error) {
	if c.connString == "" {
		return "", fmt.Errorf("conn string not set; did postgres start correctly")
	}
	return c.connString, nil
}

// Stop stops the running container, if any.
func (c *Client) Stop(ctx context.Context) error { return c.db.Stop(ctx) }
