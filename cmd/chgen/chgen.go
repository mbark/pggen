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
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/mbark/pggen"
	"github.com/mbark/pggen/internal/flags"
	"github.com/mbark/pggen/internal/paths"
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

func run() error {
	rootFlagSet := flag.NewFlagSet("root", flag.ExitOnError)
	rootCmd := &ffcli.Command{
		ShortUsage: "chgen <subcommand> [options...]",
		LongHelp:   flagHelp,
		FlagSet:    rootFlagSet,
		Subcommands: []*ffcli.Command{
			newGenCmd(),
			newVersionCmd(),
		},
	}
	rootCmd.Exec = func(ctx context.Context, args []string) error {
		fmt.Println(ffcli.DefaultUsageFunc(rootCmd))
		os.Exit(1)
		return nil
	}
	return rootCmd.ParseAndRun(context.Background(), os.Args[1:])
}

func newVersionCmd() *ffcli.Command {
	return &ffcli.Command{
		Name:       "version",
		ShortUsage: "prints chgen version",
		Exec: func(ctx context.Context, args []string) error {
			fmt.Printf("chgen version %s, commit %s\n", version, commit)
			return nil
		},
	}
}

func newGenCmd() *ffcli.Command {
	fset := flag.NewFlagSet("go", flag.ExitOnError)
	outputDir := fset.String("output-dir", "",
		"where to write generated code; defaults to same directory as query files")
	clickhouseConn := fset.String("clickhouse-connection", "",
		`optional connection string to a ClickHouse database, like: `+
			`"clickhouse://default:hunter2@localhost:9000/pggen"`)
	queryGlobs := flags.Strings(fset, "query-glob", nil,
		"generate code for all SQL files that match glob, like 'queries/**/*.sql'")
	schemaGlobs := flags.Strings(fset, "schema-glob", nil,
		"create schema in ClickHouse from all sql files that match a glob, "+
			"like 'migrations/*.sql'")
	acronyms := flags.Strings(fset, "acronym", nil,
		"lowercase acronym that should convert to all caps like 'api', "+
			"or custom mapping like 'apis=APIs'")
	goTypes := flags.Strings(fset, "go-type", nil,
		"custom type mapping from ClickHouse to fully qualified Go type, "+
			"like 'UUID=github.com/gofrs/uuid.UUID'")
	inlineParamCount := fset.Int("inline-param-count", 2,
		"number of params (inclusive) to inline when calling querier methods; 0 always generates a struct")

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
			if len(*queryGlobs) == 0 {
				return fmt.Errorf("chgen gen go: at least one file in --query-glob must match")
			}
			queries, err := paths.ExpandSortGlobs(*queryGlobs)
			if err != nil {
				return err
			}
			schemas, err := paths.ExpandSortGlobs(*schemaGlobs)
			if err != nil {
				return err
			}

			// Deduce output directory.
			outDir := *outputDir
			if outDir == "" {
				for _, file := range queries {
					dir := filepath.Dir(file)
					if outDir != "" && dir != outDir {
						return fmt.Errorf("cannot deduce output dir because query files use different dirs; " +
							"specify explicitly with --output-dir")
					}
					outDir = dir
				}
			}
			outDir, _ = filepath.Abs(outDir)

			// Parse two acronym formats: "--acronym api" and "--acronym oids=OIDs"
			acros := make(map[string]string)
			for _, acro := range *acronyms {
				ss := strings.SplitN(acro, "=", 2)
				word := ss[0]
				if word != strings.ToLower(word) {
					return fmt.Errorf("acronym %q should be lower case", word)
				}
				replacement := strings.ToUpper(word)
				if len(ss) > 1 {
					replacement = ss[1]
				}
				acros[word] = replacement
			}

			typeOverrides := make(map[string]string, len(*goTypes))
			for _, typeAssoc := range *goTypes {
				if strings.Count(typeAssoc, "=") != 1 {
					return fmt.Errorf("--go-type must have format <chType>=<goType>; got %s", typeAssoc)
				}
				ss := strings.SplitN(typeAssoc, "=", 2)
				typeOverrides[ss[0]] = ss[1]
			}

			err = pggen.Generate(pggen.GenerateOptions{
				Language:         pggen.LangGo,
				Dialect:          pggen.DialectClickHouse,
				ConnString:       *clickhouseConn,
				SchemaFiles:      schemas,
				QueryFiles:       queries,
				OutputDir:        outDir,
				Acronyms:         acros,
				TypeOverrides:    typeOverrides,
				LogLevel:         slog.LevelInfo,
				InlineParamCount: *inlineParamCount,
			})
			if err != nil {
				return err
			}

			fileDesc := "files"
			if len(queries) == 1 {
				fileDesc = "file"
			}
			fmt.Printf("generated %d query %s\n", len(queries), fileDesc)
			return nil
		},
	}

	cmd := &ffcli.Command{
		Name:        "gen",
		ShortUsage:  "chgen gen (go|<lang>) [options...]",
		ShortHelp:   "generates code in specific language for ClickHouse query files",
		FlagSet:     nil,
		Subcommands: []*ffcli.Command{goSubCmd},
	}
	cmd.Exec = func(ctx context.Context, args []string) error {
		fmt.Println(ffcli.DefaultUsageFunc(cmd))
		os.Exit(1)
		return nil
	}
	return cmd
}

func main() {
	if err := run(); err != nil {
		fmt.Printf("ERROR: %s\n", err.Error())
		os.Exit(1)
	}
}
