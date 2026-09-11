package golang

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateSQLConstNames covers the check that stands between a mistaken
// sql= pragma and a redeclaration error in the generated code, which is the one
// place an error is expensive to read.
func TestValidateSQLConstNames(t *testing.T) {
	tests := []struct {
		name    string
		files   []TemplatedFile
		wantErr string
	}{
		{
			name: "two names that differ",
			files: []TemplatedFile{{SourcePath: "a.sql", Queries: []TemplatedQuery{
				{Name: "FindItems", SQLConst: "FindItemsSQL"},
				{Name: "FindUsers", SQLConst: "FindUsersSQL"},
			}}},
		},
		{
			name: "the same name in two files",
			files: []TemplatedFile{
				{SourcePath: "a.sql", Queries: []TemplatedQuery{{Name: "FindItems", SQLConst: "QuerySQL"}}},
				{SourcePath: "b.sql", Queries: []TemplatedQuery{{Name: "FindUsers", SQLConst: "QuerySQL"}}},
			},
			wantErr: "two queries both declare sql=QuerySQL",
		},
		{
			// The row struct is generated from the query name, so a constant
			// named after it collides with a declaration the caller never wrote.
			name: "a name that is another query's row struct",
			files: []TemplatedFile{{SourcePath: "a.sql", Queries: []TemplatedQuery{
				{Name: "FindItems"},
				{Name: "FindUsers", SQLConst: "FindItemsRow"},
			}}},
			wantErr: "which is also the row struct of FindItems",
		},
		{
			name: "a name that is a shared output= struct",
			files: []TemplatedFile{{SourcePath: "a.sql", Queries: []TemplatedQuery{
				{Name: "FindItems", OutputType: "ItemRow"},
				{Name: "FindUsers", SQLConst: "ItemRow"},
			}}},
			wantErr: "which is also the row struct of FindItems",
		},
		{
			name: "a name that is a params struct",
			files: []TemplatedFile{{SourcePath: "a.sql", Queries: []TemplatedQuery{
				{Name: "FindItems"},
				{Name: "FindUsers", SQLConst: "FindItemsParams"},
			}}},
			wantErr: "which is also the params struct of FindItems",
		},
		{
			name: "a name that is a paginate group's params struct",
			files: []TemplatedFile{{SourcePath: "a.sql",
				Queries:  []TemplatedQuery{{Name: "FindUsers", SQLConst: "PageItemsParams"}},
				Variants: []TemplatedQuery{{Name: "PageItems", VariantGroup: "PageItems"}},
			}},
			wantErr: "which is also the params struct of PageItems",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateSQLConstNames(tt.files)
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}
