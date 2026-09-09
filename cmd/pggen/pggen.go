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

var flagHelp = `pggen generates type-safe code from files containing Postgres queries by running
the queries on Postgres to get type information.

EXAMPLES
  # Generate code for a single query file using an existing postgres database.
  pggen gen go --query-glob author/queries.sql --postgres-connection "user=postgres port=5555 dbname=pggen"

  # Generate code using Docker to create the postgres database with a schema 
  # file. --schema-glob arg implies using Dockerized postgres.
  pggen gen go --schema-glob author/schema.sql --query-glob author/queries.sql

  # Generate code for all queries underneath a directory. Glob should be quoted
  # to prevent shell expansion.
  pggen gen go --schema-glob author/schema.sql --query-glob 'author/**/*.sql'

  # Use custom acronym when converting from camel_case_api to camelCaseAPI.
  pggen gen go --schema-glob schema.sql --query-glob query.sql --acronym api
`

var labels = cli.Labels{
	Cmd:      "pggen",
	DB:       "Postgres",
	TypeName: "<pgType>",
	SchemaHelp: "create schema in Postgres from all sql, sql.gz, or shell " +
		"scripts (*.sh) that match a glob, like 'migrations/*.sql'",
	GoTypeHelp: "custom type mapping from Postgres to fully qualified Go type, " +
		"like 'device_type=github.com/mbark/pggen.DeviceType'",
}

func newGenCmd() *ffcli.Command {
	fset := flag.NewFlagSet("go", flag.ExitOnError)
	postgresConn := fset.String("postgres-connection", "",
		`optional connection string to a postgres database, like: `+
			`"user=postgres host=localhost dbname=pggen"`)
	genFlags := cli.RegisterGenFlags(fset, labels)

	goSubCmd := &ffcli.Command{
		Name:       "go",
		ShortUsage: "pggen gen go --query-glob glob [--schema-glob <glob>]... [flags]",
		ShortHelp:  "generates go code for Postgres query files",
		FlagSet:    fset,
		LongHelp: flagHelp + "\n" + texts.Dedent(`
			pggen uses the provided --postgres-connection to query the database. If not 
			present, pggen creates a Docker container to query the database.
		`),
		Exec: func(ctx context.Context, args []string) error {
			gen, err := genFlags.Resolve()
			if err != nil {
				return err
			}
			err = pggen.Generate(pggen.GenerateOptions{
				Language:         pggen.LangGo,
				Dialect:          pggen.DialectPostgres,
				ConnString:       *postgresConn,
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
