// Command chgen generates type-safe Go code from ClickHouse SQL queries.
//
// It is pggen's sibling: same query file format, same pragmas, same generated
// shape, pointed at ClickHouse instead of Postgres. The two are separate
// binaries rather than one with a flag because they target different
// databases, different drivers, and different query syntax for parameters.
package main

import (
	"context"
	"flag"
	"log/slog"

	"github.com/mbark/pggen"
	"github.com/mbark/pggen/internal/cli"
	"github.com/mbark/pggen/internal/texts"
	"github.com/peterbourgon/ff/v3/ffcli"
)

// Set via ldflags for release binaries.
var (
	version = "dev"
	commit  = "head"
)

var flagHelp = `chgen generates type-safe code from files containing ClickHouse queries by
asking ClickHouse to describe each query.

Declare inputs with ClickHouse's own parameter syntax, {name:Type}, which means
a query file is also a query you can paste into clickhouse-client:

  -- name: FindUsage :many
  SELECT a_num, sum(units) AS data_bytes
  FROM cdr
  WHERE a_num IN {msisdns:Array(String)}
    AND start_date >= {from:DateTime}
  GROUP BY a_num;

EXAMPLES
  # Generate code for a single query file using an existing ClickHouse.
  chgen gen go --query-glob cdr/queries.sql \
      --clickhouse-connection "clickhouse://default:hunter2@localhost:9000/pggen"

  # Generate code using Docker to create ClickHouse from a schema file.
  # --schema-glob implies using Dockerized ClickHouse.
  chgen gen go --schema-glob cdr/schema.sql --query-glob cdr/queries.sql

  # Generate code for all queries underneath a directory. Glob should be quoted
  # to prevent shell expansion.
  chgen gen go --schema-glob cdr/schema.sql --query-glob 'cdr/**/*.sql'

  # Use custom acronym when converting from camel_case_api to camelCaseAPI.
  chgen gen go --schema-glob schema.sql --query-glob query.sql --acronym api
`

var labels = cli.Labels{
	Cmd:      "chgen",
	DB:       "ClickHouse",
	TypeName: "<chType>",
	SchemaHelp: "create schema in ClickHouse from all sql files that match a glob, " +
		"like 'migrations/*.sql'",
	GoTypeHelp: "custom type mapping from ClickHouse to fully qualified Go type, " +
		"like 'UUID=github.com/gofrs/uuid.UUID'",
}

func newGenCmd() *ffcli.Command {
	fset := flag.NewFlagSet("go", flag.ExitOnError)
	clickhouseConn := fset.String("clickhouse-connection", "",
		`optional connection string to a ClickHouse database, like: `+
			`"clickhouse://default:hunter2@localhost:9000/pggen"`)
	genFlags := cli.RegisterGenFlags(fset, labels)

	goSubCmd := &ffcli.Command{
		Name:       "go",
		ShortUsage: "chgen gen go --query-glob glob [--schema-glob <glob>]... [flags]",
		ShortHelp:  "generates go code for ClickHouse query files",
		FlagSet:    fset,
		LongHelp: flagHelp + "\n" + texts.Dedent(`
			chgen uses the provided --clickhouse-connection to query the database. If not
			present, chgen creates a Docker container to query the database.
		`),
		Exec: func(ctx context.Context, args []string) error {
			gen, err := genFlags.Resolve()
			if err != nil {
				return err
			}
			err = pggen.Generate(pggen.GenerateOptions{
				Language:         pggen.LangGo,
				Dialect:          pggen.DialectClickHouse,
				ConnString:       *clickhouseConn,
				SchemaFiles:      gen.SchemaFiles,
				QueryFiles:       gen.QueryFiles,
				OutputDir:        gen.OutputDir,
				Acronyms:         gen.Acronyms,
				TypeOverrides:    gen.TypeOverrides,
				LogLevel:         slog.LevelInfo,
				InlineParamCount: gen.InlineParamCount,
			})
			if err != nil {
				return err
			}
			cli.ReportGenerated(len(gen.QueryFiles))
			return nil
		},
	}
	return cli.GenCmd(labels, goSubCmd)
}

func main() {
	cli.Main(cli.RootCmd(labels, flagHelp,
		newGenCmd(),
		cli.VersionCmd(labels, version, commit),
	))
}
