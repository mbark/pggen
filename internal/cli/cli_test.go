package cli

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseGoTypes(t *testing.T) {
	tests := []struct {
		name  string
		flags []string
		want  map[string]string
	}{
		{
			name:  "none",
			flags: nil,
			want:  map[string]string{},
		},
		{
			name:  "a plain type",
			flags: []string{"String=example.com/brand.Name"},
			want:  map[string]string{"String": "example.com/brand.Name"},
		},
		{
			// The type a user most wants to override is the one that carries
			// "=" in its own name, so splitting on the only "=" rejected the
			// documented form.
			name:  "an enum, whose type spells its labels",
			flags: []string{"Enum8('MOC' = 1, 'GPRS' = 7)=example.com/cdr.RecordType"},
			want: map[string]string{
				"Enum8('MOC' = 1, 'GPRS' = 7)": "example.com/cdr.RecordType",
			},
		},
		{
			name:  "a parameterised type",
			flags: []string{"Decimal(18, 6)=float64"},
			want:  map[string]string{"Decimal(18, 6)": "float64"},
		},
		{
			name: "several",
			flags: []string{
				"IPv4=string",
				"Enum8('a' = 1)=example.com/cdr.Kind",
			},
			want: map[string]string{
				"IPv4":           "string",
				"Enum8('a' = 1)": "example.com/cdr.Kind",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseGoTypes(tt.flags)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseGoTypes_errors(t *testing.T) {
	for _, flag := range []string{"String", "=example.com/brand.Name", "String=", ""} {
		t.Run(flag, func(t *testing.T) {
			_, err := ParseGoTypes([]string{flag})
			assert.ErrorContains(t, err, "--go-type must have format")
		})
	}
}

func TestParseAcronyms(t *testing.T) {
	tests := []struct {
		name  string
		flags []string
		want  map[string]string
	}{
		{name: "none", flags: nil, want: map[string]string{}},
		{
			name:  "a bare word upper-cases",
			flags: []string{"api"},
			want:  map[string]string{"api": "API"},
		},
		{
			name:  "an explicit mapping keeps its case",
			flags: []string{"apis=APIs"},
			want:  map[string]string{"apis": "APIs"},
		},
		{
			name:  "several",
			flags: []string{"msisdn=MSISDN", "imsi=IMSI", "cdr"},
			want: map[string]string{
				"msisdn": "MSISDN",
				"imsi":   "IMSI",
				"cdr":    "CDR",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseAcronyms(tt.flags)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseAcronyms_rejectsAnUpperCaseWord(t *testing.T) {
	_, err := ParseAcronyms([]string{"API=API"})
	assert.ErrorContains(t, err, "should be lower case")
}

func TestDeduceOutputDir(t *testing.T) {
	t.Run("an explicit dir wins and is made absolute", func(t *testing.T) {
		got, err := DeduceOutputDir("out", []string{"a/one.sql", "b/two.sql"})
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(got), "want an absolute path, got %s", got)
		assert.Equal(t, "out", filepath.Base(got))
	})

	t.Run("one dir is deduced from the query files", func(t *testing.T) {
		got, err := DeduceOutputDir("", []string{"/tmp/q/one.sql", "/tmp/q/two.sql"})
		require.NoError(t, err)
		assert.Equal(t, "/tmp/q", got)
	})

	t.Run("query files in different dirs have no single answer", func(t *testing.T) {
		_, err := DeduceOutputDir("", []string{"/tmp/a/one.sql", "/tmp/b/two.sql"})
		assert.ErrorContains(t, err, "cannot deduce output dir")
	})
}
