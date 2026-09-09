// Package cli holds the command-line plumbing that pggen and chgen share.
//
// The two binaries stay separate because they target different databases with
// different drivers and different parameter syntax. What they do with the
// flags, though, is the same in both: expand the globs, deduce an output
// directory, parse the acronym and type-override formats, and print how many
// files came out. That belongs in one place, so a fix lands in both.
package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mbark/pggen/internal/flags"
	"github.com/mbark/pggen/internal/paths"
	"github.com/peterbourgon/ff/v3/ffcli"
)

// Labels are the words that differ between the two binaries: the command
// name, the database it talks to, and the help text for the flags whose
// wording is database-specific.
type Labels struct {
	Cmd        string // "pggen" or "chgen"
	DB         string // "Postgres" or "ClickHouse"
	TypeName   string // "<pgType>" or "<chType>", used in --go-type errors
	SchemaHelp string // help for --schema-glob
	GoTypeHelp string // help for --go-type
}

// GenFlags are the flags common to both gen commands.
type GenFlags struct {
	labels           Labels
	outputDir        *string
	queryGlobs       *[]string
	schemaGlobs      *[]string
	acronyms         *[]string
	goTypes          *[]string
	inlineParamCount *int
}

// RegisterGenFlags declares the shared gen flags on fset. The connection flag
// is not among them: each binary names it after its own database.
func RegisterGenFlags(fset *flag.FlagSet, labels Labels) *GenFlags {
	return &GenFlags{
		labels: labels,
		outputDir: fset.String("output-dir", "",
			"where to write generated code; defaults to same directory as query files"),
		queryGlobs: flags.Strings(fset, "query-glob", nil,
			"generate code for all SQL files that match glob, like 'queries/**/*.sql'"),
		schemaGlobs: flags.Strings(fset, "schema-glob", nil, labels.SchemaHelp),
		acronyms: flags.Strings(fset, "acronym", nil,
			"lowercase acronym that should convert to all caps like 'api', "+
				"or custom mapping like 'apis=APIs'"),
		goTypes: flags.Strings(fset, "go-type", nil, labels.GoTypeHelp),
		inlineParamCount: fset.Int("inline-param-count", 2,
			"number of params (inclusive) to inline when calling querier methods; 0 always generates a struct"),
	}
}

// Gen is the resolved form of the shared flags, ready to hand to
// pggen.Generate.
type Gen struct {
	QueryFiles       []string
	SchemaFiles      []string
	OutputDir        string
	Acronyms         map[string]string
	TypeOverrides    map[string]string
	InlineParamCount int
}

// Resolve expands the globs and parses the flags whose values carry their own
// little formats.
func (f *GenFlags) Resolve() (Gen, error) {
	if len(*f.queryGlobs) == 0 {
		return Gen{}, fmt.Errorf("%s gen go: at least one file in --query-glob must match", f.labels.Cmd)
	}
	queries, err := paths.ExpandSortGlobs(*f.queryGlobs)
	if err != nil {
		return Gen{}, err
	}
	schemas, err := paths.ExpandSortGlobs(*f.schemaGlobs)
	if err != nil {
		return Gen{}, err
	}
	outDir, err := DeduceOutputDir(*f.outputDir, queries)
	if err != nil {
		return Gen{}, err
	}
	acronyms, err := ParseAcronyms(*f.acronyms)
	if err != nil {
		return Gen{}, err
	}
	typeOverrides, err := ParseGoTypes(f.labels.TypeName, *f.goTypes)
	if err != nil {
		return Gen{}, err
	}
	return Gen{
		QueryFiles:       queries,
		SchemaFiles:      schemas,
		OutputDir:        outDir,
		Acronyms:         acronyms,
		TypeOverrides:    typeOverrides,
		InlineParamCount: *f.inlineParamCount,
	}, nil
}

// DeduceOutputDir returns outputDir if set, and otherwise the directory the
// query files live in. Query files spread across directories have no single
// answer, so that's an error rather than a guess.
func DeduceOutputDir(outputDir string, queryFiles []string) (string, error) {
	outDir := outputDir
	if outDir == "" {
		for _, file := range queryFiles {
			dir := filepath.Dir(file)
			if outDir != "" && dir != outDir {
				return "", fmt.Errorf("cannot deduce output dir because query files use different dirs; " +
					"specify explicitly with --output-dir")
			}
			outDir = dir
		}
	}
	abs, err := filepath.Abs(outDir)
	if err != nil {
		return "", fmt.Errorf("resolve output dir %s: %w", outDir, err)
	}
	return abs, nil
}

// ParseAcronyms reads the --acronym flag, which takes either a bare word to
// upper-case ("api") or an explicit mapping ("apis=APIs").
func ParseAcronyms(acronyms []string) (map[string]string, error) {
	acros := make(map[string]string, len(acronyms))
	for _, acro := range acronyms {
		ss := strings.SplitN(acro, "=", 2)
		word := ss[0]
		if word != strings.ToLower(word) {
			return nil, fmt.Errorf("acronym %q should be lower case", word)
		}
		replacement := strings.ToUpper(word)
		if len(ss) > 1 {
			replacement = ss[1]
		}
		acros[word] = replacement
	}
	return acros, nil
}

// ParseGoTypes reads the --go-type flag, whose format is <dbType>=<goType>.
//
// It splits on the last "=" rather than insisting on the only one, because a
// database type can contain "=" and a ClickHouse enum routinely does: it
// spells its labels in the type, as Enum8('MOC' = 1, 'GPRS' = 7), and that is
// exactly the type most worth overriding. A Go type reference cannot contain
// "=", so the last one is always the separator.
//
// typeName names the left-hand side in the error message, like "<pgType>".
func ParseGoTypes(typeName string, goTypes []string) (map[string]string, error) {
	overrides := make(map[string]string, len(goTypes))
	for _, typeAssoc := range goTypes {
		i := strings.LastIndex(typeAssoc, "=")
		if i <= 0 || i == len(typeAssoc)-1 {
			return nil, fmt.Errorf(
				"--go-type must have format %s=<goType>; got %q", typeName, typeAssoc)
		}
		overrides[typeAssoc[:i]] = typeAssoc[i+1:]
	}
	return overrides, nil
}

// ReportGenerated prints the count of generated query files.
func ReportGenerated(n int) {
	fileDesc := "files"
	if n == 1 {
		fileDesc = "file"
	}
	fmt.Printf("generated %d query %s\n", n, fileDesc)
}

// RootCmd builds the top-level command, which only prints usage.
func RootCmd(labels Labels, longHelp string, subcommands ...*ffcli.Command) *ffcli.Command {
	cmd := &ffcli.Command{
		ShortUsage:  labels.Cmd + " <subcommand> [options...]",
		LongHelp:    longHelp,
		FlagSet:     flag.NewFlagSet("root", flag.ExitOnError),
		Subcommands: subcommands,
	}
	cmd.Exec = usageExec(cmd)
	return cmd
}

// GenCmd builds the "gen" command, which only dispatches to a language.
func GenCmd(labels Labels, langCmds ...*ffcli.Command) *ffcli.Command {
	cmd := &ffcli.Command{
		Name:        "gen",
		ShortUsage:  labels.Cmd + " gen (go|<lang>) [options...]",
		ShortHelp:   "generates code in specific language for " + labels.DB + " query files",
		Subcommands: langCmds,
	}
	cmd.Exec = usageExec(cmd)
	return cmd
}

// VersionCmd builds the "version" command.
func VersionCmd(labels Labels, version, commit string) *ffcli.Command {
	return &ffcli.Command{
		Name:       "version",
		ShortUsage: "prints " + labels.Cmd + " version",
		Exec: func(ctx context.Context, args []string) error {
			fmt.Printf("%s version %s, commit %s\n", labels.Cmd, version, commit)
			return nil
		},
	}
}

func usageExec(cmd *ffcli.Command) func(context.Context, []string) error {
	return func(ctx context.Context, args []string) error {
		fmt.Println(ffcli.DefaultUsageFunc(cmd))
		os.Exit(1)
		return nil
	}
}

// Main runs root and turns an error into a message and a non-zero exit.
func Main(root *ffcli.Command) {
	if err := root.ParseAndRun(context.Background(), os.Args[1:]); err != nil {
		fmt.Printf("ERROR: %s\n", err.Error())
		os.Exit(1)
	}
}
