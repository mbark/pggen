package pggen

import (
	"context"
	"errors"
	"fmt"
	gotok "go/token"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/ClickHouse/clickhouse-go/v2"
	"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
	"github.com/jackc/pgx/v5"
	"github.com/mbark/pggen/internal/ast"
	"github.com/mbark/pggen/internal/chdocker"
	"github.com/mbark/pggen/internal/chinfer"
	"github.com/mbark/pggen/internal/codegen"
	"github.com/mbark/pggen/internal/codegen/golang"
	"github.com/mbark/pggen/internal/errs"
	"github.com/mbark/pggen/internal/parser"
	"github.com/mbark/pggen/internal/pgdocker"
	"github.com/mbark/pggen/internal/pginfer"
)

// Lang is a supported codegen language.
type Lang string

const (
	LangGo Lang = "go"
)

// Dialect is a supported database backend. It is orthogonal to Lang: Lang
// picks the language of the generated code, Dialect picks the database the
// queries run against and therefore how their types are inferred.
type Dialect = codegen.Dialect

const (
	DialectPostgres   = codegen.DialectPostgres
	DialectClickHouse = codegen.DialectClickHouse
)

// Inferrer turns a parsed query into a typed query by asking the database
// about the query's parameter and result types. Each dialect has its own
// implementation: pginfer prepares the query and reads Postgres OIDs back,
// chinfer reads ClickHouse type names out of DESCRIBE.
type Inferrer interface {
	InferTypes(query *ast.SourceQuery) (codegen.TypedQuery, error)
}

// GenerateOptions are the unparsed options that controls the generated Go code.
type GenerateOptions struct {
	// What language to generate code in.
	Language Lang
	// Which database the queries run against. Defaults to DialectPostgres.
	Dialect Dialect
	// The connection string to the running Postgres database to use to get type
	// information for each query in QueryFiles.
	//
	// Must be parseable by pgconn.ParseConfig, like:
	//
	//   # Example DSN
	//   user=jack password=secret host=pg.example.com port=5432 dbname=foo_db sslmode=verify-ca
	//
	//   # Example URL
	//   postgres://jack:secret@pg.example.com:5432/foo_db?sslmode=verify-ca
	ConnString string
	// Generate code for each of the SQL query file paths.
	QueryFiles []string
	// Schema files to run on Postgres init. Can be *.sql, *.sql.gz, or executable
	// *.sh files .
	SchemaFiles []string
	// The name of the Go package for the file. If empty, defaults to the
	// directory name.
	GoPackage string
	// Directory to write generated files. Writes one file for each query file.
	// If more than one query file, also writes querier.go.
	OutputDir string
	// A map of lowercase acronyms to the upper case equivalent, like:
	// "api" => "API", or "apis" => "APIs".
	Acronyms map[string]string
	// A map from a Postgres type name to a fully qualified Go type.
	TypeOverrides map[string]string
	// What log level to log at.
	LogLevel slog.Level
	// How many params to inline when calling querier methods.
	// Set to 0 to always create a struct for params.
	InlineParamCount int
}

// Generate generates language specific code to safely wrap each SQL
// ast.SourceQuery in opts.QueryFiles.
//
// Generate must only be called once per output directory.
func Generate(opts GenerateOptions) (mErr error) {
	// Preconditions.
	if opts.Language == "" {
		return fmt.Errorf("generate language must be set; got empty string")
	}
	if len(opts.QueryFiles) == 0 {
		return fmt.Errorf("got 0 query files, at least 1 must be set")
	}
	if opts.OutputDir == "" {
		return fmt.Errorf("output dir must be set")
	}
	if opts.Dialect == "" {
		opts.Dialect = DialectPostgres
	}

	// Database connection.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	inferrer, errEnricher, cleanup, err := connectInferrer(ctx, opts)
	if err != nil {
		return err
	}
	defer errs.Capture(&mErr, cleanup, "close database connection")

	// Parse queries.
	queryFiles, err := parseQueryFiles(opts.QueryFiles, inferrer, opts.Dialect)
	if err != nil {
		return errEnricher(err)
	}

	// Codegen.
	if opts.Acronyms == nil {
		opts.Acronyms = make(map[string]string, 1)
	}
	if _, ok := opts.Acronyms["id"]; !ok {
		opts.Acronyms["id"] = "ID"
	}
	switch opts.Language {
	case LangGo:
		goOpts := golang.GenerateOptions{
			Dialect:          opts.Dialect,
			GoPkg:            opts.GoPackage,
			OutputDir:        opts.OutputDir,
			Acronyms:         opts.Acronyms,
			TypeOverrides:    opts.TypeOverrides,
			InlineParamCount: opts.InlineParamCount,
		}
		if err := golang.Generate(goOpts, queryFiles); err != nil {
			return fmt.Errorf("generate go code: %w", err)
		}
	default:
		return fmt.Errorf("unsupported output language %q", opts.Language)
	}
	return nil
}

// connectInferrer connects to the database for opts.Dialect and returns an
// Inferrer backed by it, along with an error enricher and a cleanup func.
func connectInferrer(ctx context.Context, opts GenerateOptions) (Inferrer, func(error) error, func() error, error) {
	switch opts.Dialect {
	case DialectPostgres:
		pgConn, errEnricher, cleanup, err := connectPostgres(ctx, opts)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("connect postgres: %w", err)
		}
		return pginfer.NewInferrer(pgConn), errEnricher, cleanup, nil
	case DialectClickHouse:
		conn, cleanup, err := connectClickHouse(ctx, opts)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("connect clickhouse: %w", err)
		}
		return chinfer.NewInferrer(conn), func(e error) error { return e }, cleanup, nil
	default:
		return nil, nil, nil, fmt.Errorf("unsupported dialect %q", opts.Dialect)
	}
}

// connectClickHouse connects to ClickHouse using connString if given, or by
// running a Docker ClickHouse container and connecting to that. Schema files
// are loaded before any query is inferred.
func connectClickHouse(ctx context.Context, opts GenerateOptions) (driver.Conn, func() error, error) {
	connString := opts.ConnString
	stop := func() error { return nil }
	if connString == "" {
		client, err := chdocker.Start(ctx, opts.SchemaFiles)
		if err != nil {
			return nil, nil, fmt.Errorf("start dockerized clickhouse: %w", err)
		}
		stop = func() error { return client.Stop(ctx) }
		connString = client.ConnString()
	}

	chOpts, err := clickhouse.ParseDSN(connString)
	if err != nil {
		_ = stop()
		return nil, nil, fmt.Errorf("parse clickhouse connection string: %w", err)
	}
	conn, err := clickhouse.Open(chOpts)
	if err != nil {
		_ = stop()
		return nil, nil, fmt.Errorf("open clickhouse connection: %w", err)
	}
	if err := conn.Ping(ctx); err != nil {
		_ = stop()
		return nil, nil, fmt.Errorf("ping clickhouse: %w", err)
	}

	// With an external connection, load the schema files ourselves; the Docker
	// path has already run them as init scripts.
	if opts.ConnString != "" {
		if err := loadClickHouseSchemas(ctx, conn, opts.SchemaFiles); err != nil {
			_ = conn.Close()
			return nil, nil, err
		}
	}

	cleanup := func() error {
		closeErr := conn.Close()
		if stopErr := stop(); stopErr != nil {
			return stopErr
		}
		return closeErr
	}
	return conn, cleanup, nil
}

// loadClickHouseSchemas runs each schema file against conn. ClickHouse runs
// one statement per call, so the files are split on semicolons.
func loadClickHouseSchemas(ctx context.Context, conn driver.Conn, schemaFiles []string) error {
	for _, file := range schemaFiles {
		if filepath.Ext(file) != ".sql" {
			return fmt.Errorf("clickhouse schema file %s must be a .sql file", file)
		}
		bs, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read clickhouse schema file %s: %w", file, err)
		}
		for _, stmt := range chdocker.SplitStatements(string(bs)) {
			if err := conn.Exec(ctx, stmt); err != nil {
				return fmt.Errorf("run clickhouse schema file %s: %w", file, err)
			}
		}
	}
	return nil
}

// connectPostgres connects to postgres using connString if given or by
// running a Docker postgres container and connecting to that.
func connectPostgres(ctx context.Context, opts GenerateOptions) (*pgx.Conn, func(error) error, func() error, error) {
	// Create connection by starting dockerized Postgres.
	if opts.ConnString == "" {
		client, err := pgdocker.Start(ctx, opts.SchemaFiles)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("start dockerized postgres: %w", err)
		}
		stopDocker := func() error { return client.Stop(ctx) }
		connStr, err := client.ConnString()
		if err != nil {
			return nil, nil, nil, fmt.Errorf("get dockerized postgres conn string: %w", err)
		}
		pgConn, err := pgx.Connect(ctx, connStr)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("connect to pggen dockerized postgres database: %w", err)
		}
		errEnricher := func(e error) error {
			if e == nil {
				return e
			}
			logs, err := client.GetContainerLogs()
			if err != nil {
				return errors.Join(e, err)
			}
			return fmt.Errorf("container logs for Postgres container:\n\n%s\n\n%w", logs, e)
		}
		return pgConn, errEnricher, stopDocker, nil
	}
	// Use existing Postgres.
	nopCleanup := func() error { return nil }
	nopErrEnricher := func(e error) error { return e }
	pgConn, err := pgx.Connect(ctx, opts.ConnString)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("connect to pggen postgres database: %w", err)
	}
	// Run SQL init scripts. pgdocker runs these in the other case by copying
	// the files into the entrypoint folder. Emulate the behavior for a subset of
	// supported files.
	for _, script := range opts.SchemaFiles {
		if filepath.Ext(script) != ".sql" {
			return nil, nopErrEnricher, nopCleanup, fmt.Errorf("cannot run non-sql schema file on Postgres "+
				"(*.sh and *.sql.gz files only supported without --postgres-connection): %s", script)
		}
		bs, err := os.ReadFile(script)
		if err != nil {
			return nil, nil, nopCleanup, fmt.Errorf("read schema file: %w", err)
		}
		if _, err := pgConn.Exec(ctx, string(bs)); err != nil {
			return nil, nopErrEnricher, nopCleanup, fmt.Errorf("load schema file into Postgres: %w", err)
		}
	}
	return pgConn, nopErrEnricher, nopCleanup, nil
}

func parseQueryFiles(queryFiles []string, inferrer Inferrer, dialect Dialect) ([]codegen.QueryFile, error) {
	files := make([]codegen.QueryFile, len(queryFiles))
	for i, file := range queryFiles {
		srcPath, err := filepath.Abs(file)
		if err != nil {
			return nil, fmt.Errorf("resolve absolute path for %q: %w", file, err)
		}
		queryFile, err := parseQueries(srcPath, inferrer, dialect)
		if err != nil {
			return nil, fmt.Errorf("parse template query file %q: %w", file, err)
		}
		files[i] = queryFile
	}
	return files, nil
}

func parseQueries(srcPath string, inferrer Inferrer, dialect Dialect) (codegen.QueryFile, error) {
	placeholder := parser.PostgresPlaceholder
	if dialect == DialectClickHouse {
		placeholder = parser.ClickHousePlaceholder
	}
	astFile, err := parser.ParseFileDialect(gotok.NewFileSet(), srcPath, nil, 0, placeholder)
	if err != nil {
		return codegen.QueryFile{}, fmt.Errorf("parse query file %q: %w", srcPath, err)
	}

	// Check for duplicate query names and bad queries.
	srcQueries := make([]*ast.SourceQuery, 0, len(astFile.Queries))
	seenNames := make(map[string]struct{}, len(astFile.Queries))
	for _, query := range astFile.Queries {
		switch query := query.(type) {
		case *ast.BadQuery:
			return codegen.QueryFile{}, errors.New("parsed bad query instead of erroring")
		case *ast.SourceQuery:
			if _, ok := seenNames[query.Name]; ok {
				return codegen.QueryFile{}, fmt.Errorf("duplicate query name %s", query.Name)
			}
			if dialect == DialectClickHouse && len(query.ParamNames) > 0 {
				return codegen.QueryFile{}, fmt.Errorf(
					"query %s uses pggen.arg(%q), which pggen does not support for ClickHouse; "+
						"declare the parameter natively instead, like {%s:String}, "+
						"since ClickHouse parameters carry their own type",
					query.Name, query.ParamNames[0], query.ParamNames[0])
			}
			seenNames[query.Name] = struct{}{}
			srcQueries = append(srcQueries, query)
		default:
			return codegen.QueryFile{}, fmt.Errorf("unhandled query ast type: %T", query)
		}
	}

	// Infer types.
	queries := make([]codegen.TypedQuery, 0, len(astFile.Queries))
	for _, srcQuery := range srcQueries {
		typedQuery, err := inferrer.InferTypes(srcQuery)
		if err != nil {
			return codegen.QueryFile{}, fmt.Errorf("infer typed named query %s: %w", srcQuery.Name, err)
		}
		queries = append(queries, typedQuery)
	}
	return codegen.QueryFile{
		SourcePath: srcPath,
		Queries:    queries,
	}, nil
}
