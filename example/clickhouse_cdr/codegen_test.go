package clickhouse_cdr

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mbark/pggen"
	"github.com/mbark/pggen/internal/chtest"
	"github.com/stretchr/testify/assert"
)

func TestGenerate_Go_Example_ClickHouseCDR(t *testing.T) {
	_, dsn := chtest.NewClickHouseDB(t, []string{"schema.sql"})

	tmpDir := t.TempDir()
	err := pggen.Generate(
		pggen.GenerateOptions{
			ConnString:       dsn,
			Dialect:          pggen.DialectClickHouse,
			QueryFiles:       []string{"query.sql"},
			OutputDir:        tmpDir,
			GoPackage:        "clickhouse_cdr",
			Language:         pggen.LangGo,
			InlineParamCount: 2,
		})
	if err != nil {
		t.Fatalf("Generate() example/clickhouse_cdr: %s", err)
	}

	wantQueryFile := "query.sql.go"
	gotQueryFile := filepath.Join(tmpDir, "query.sql.go")
	assert.FileExists(t, gotQueryFile, "Generate() should emit query.sql.go")
	wantQueries, err := os.ReadFile(wantQueryFile)
	if err != nil {
		t.Fatalf("read wanted query.go.sql: %s", err)
	}
	gotQueries, err := os.ReadFile(gotQueryFile)
	if err != nil {
		t.Fatalf("read generated query.go.sql: %s", err)
	}
	assert.Equalf(t, string(wantQueries), string(gotQueries),
		"Got file %s; does not match contents of %s", gotQueryFile, wantQueryFile)
}
