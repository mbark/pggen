package ch

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScanInsertTarget(t *testing.T) {
	tests := []struct {
		name    string
		sql     string
		want    InsertTarget
		wantOK  bool
		comment string
	}{
		{
			name:   "qualified table with a column list",
			sql:    "INSERT INTO source.raw (path, file, row) SELECT 1, 2, 3",
			want:   InsertTarget{Table: "source.raw", Columns: []string{"path", "file", "row"}},
			wantOK: true,
		},
		{
			name:   "unqualified table",
			sql:    "INSERT INTO raw (path) VALUES ('a')",
			want:   InsertTarget{Table: "raw", Columns: []string{"path"}},
			wantOK: true,
		},
		{
			// Legal, and means every column. There is nothing to check the
			// list against, but the table still resolves.
			name:   "no column list",
			sql:    "INSERT INTO source.raw SELECT * FROM other",
			want:   InsertTarget{Table: "source.raw"},
			wantOK: true,
		},
		{
			name:   "INTO TABLE is the same statement",
			sql:    "INSERT INTO TABLE source.raw (path) SELECT 1",
			want:   InsertTarget{Table: "source.raw", Columns: []string{"path"}},
			wantOK: true,
		},
		{
			name:   "backtick quoting is dropped",
			sql:    "INSERT INTO `source`.`raw` (`path`, file) SELECT 1, 2",
			want:   InsertTarget{Table: "source.raw", Columns: []string{"path", "file"}},
			wantOK: true,
		},
		{
			name:   "keywords are case insensitive",
			sql:    "insert into source.raw (path) select 1",
			want:   InsertTarget{Table: "source.raw", Columns: []string{"path"}},
			wantOK: true,
		},
		{
			name: "comments and newlines before and inside the target",
			sql: `-- leading comment
			INSERT /* and one here */ INTO source.raw
			    (path,
			     file)
			SELECT 1, 2`,
			want:   InsertTarget{Table: "source.raw", Columns: []string{"path", "file"}},
			wantOK: true,
		},
		{
			name:   "whitespace before the query",
			sql:    "\n\t  INSERT INTO t (a) SELECT 1",
			want:   InsertTarget{Table: "t", Columns: []string{"a"}},
			wantOK: true,
		},

		// Everything below has nothing to check, which is not the same as
		// being wrong: ok is false and the caller passes the query.
		{
			name:   "a SELECT is not an insert",
			sql:    "SELECT 1",
			wantOK: false,
		},
		{
			name:   "INSERT INTO FUNCTION writes through a table function",
			sql:    "INSERT INTO FUNCTION s3('http://x/y', 'CSV') SELECT 1",
			wantOK: false,
		},
		{
			name:   "an expression in the column list is not a column",
			sql:    "INSERT INTO t (a + b) SELECT 1",
			wantOK: false,
		},
		{
			name:   "an unterminated column list",
			sql:    "INSERT INTO t (a, b",
			wantOK: false,
		},
		{
			name:   "empty input",
			sql:    "",
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ScanInsertTarget(tt.sql)
			assert.Equal(t, tt.wantOK, ok)
			if tt.wantOK {
				assert.Equal(t, tt.want, got)
			}
		})
	}
}

// The target is read off the query as written, so a parameter in the SELECT
// half must not disturb it.
func TestScanInsertTarget_withParams(t *testing.T) {
	sql := `INSERT INTO source.raw (path, file, row)
	        SELECT concat({path_prefix:String}, _path), _file, row
	        FROM s3({s3_url:String}, 'LineAsString', 'row String')`
	got, ok := ScanInsertTarget(sql)
	assert.True(t, ok)
	assert.Equal(t, InsertTarget{
		Table:   "source.raw",
		Columns: []string{"path", "file", "row"},
	}, got)
}
