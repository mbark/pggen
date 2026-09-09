package clickhouse_multi

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mbark/pggen"
	"github.com/mbark/pggen/internal/chtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerate_Go_Example_ClickHouseMulti(t *testing.T) {
	_, dsn, cleanupFunc := chtest.NewClickHouseDB(t, []string{"schema.sql"})
	defer cleanupFunc()

	tmpDir := t.TempDir()
	err := pggen.Generate(
		pggen.GenerateOptions{
			ConnString: dsn,
			Dialect:    pggen.DialectClickHouse,
			QueryFiles: []string{"alpha_query.sql", "beta_query.sql"},
			OutputDir:  tmpDir,
			GoPackage:  "clickhouse_multi",
			Language:   pggen.LangGo,
			Acronyms:   map[string]string{"msisdn": "MSISDN"},
			// Matches the chgen CLI default, which produced the committed files.
			InlineParamCount: 2,
			TypeOverrides: map[string]string{
				"Enum8('MOC' = 1, 'SMO' = 2, 'GPRS' = 7)": "github.com/mbark/pggen/example/clickhouse_multi.RecordType",
			},
		})
	if err != nil {
		t.Fatalf("Generate() example/clickhouse_multi: %s", err)
	}

	for _, name := range []string{"alpha_query.sql.go", "beta_query.sql.go"} {
		got := filepath.Join(tmpDir, name)
		assert.FileExists(t, got, "Generate() should emit %s", name)

		want, err := os.ReadFile(name)
		require.NoErrorf(t, err, "read wanted %s", name)
		gotContents, err := os.ReadFile(got)
		require.NoErrorf(t, err, "read generated %s", name)

		assert.Equalf(t, string(want), string(gotContents),
			"Got file %s; does not match contents of %s", got, name)
	}
}
