package gotype

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsImportPath(t *testing.T) {
	tests := []struct {
		pkgPath string
		want    bool
	}{
		{"time", true},
		{"net", true},
		{"encoding/json", true},
		{"github.com/jackc/pgx/v5/pgtype", true},
		{"github.com/google/uuid", true},
		// The short package name of a real package is the mistake to catch.
		{"pgtype", false},
		{"uuid", false},
		{"decimal", false},
		{"", false},
	}
	for _, tt := range tests {
		t.Run(tt.pkgPath, func(t *testing.T) {
			assert.Equal(t, tt.want, IsImportPath(tt.pkgPath))
		})
	}
}

func TestParseOpaqueType_shortPackageName(t *testing.T) {
	_, err := ParseOpaqueType("*pgtype.UUID", nil)
	require.Error(t, err, "a type qualified by a short package name is rejected")
	assert.Contains(t, err.Error(), `package path "pgtype"`)
	assert.Contains(t, err.Error(), "github.com/jackc/pgx/v5/pgtype.UUID",
		"the error shows the form that works")

	// The spelled-out form is what the short one was reaching for.
	typ, err := ParseOpaqueType("*github.com/jackc/pgx/v5/pgtype.UUID", nil)
	require.NoError(t, err)
	assert.Equal(t, "*UUID", typ.BaseName())
	assert.Equal(t, "pgtype.UUID", QualifyType(typ.(*PointerType).Elem, "", nil))
}
