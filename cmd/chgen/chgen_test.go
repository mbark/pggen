package main

import (
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
			got, err := parseGoTypes(tt.flags)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParseGoTypes_errors(t *testing.T) {
	for _, flag := range []string{"String", "=example.com/brand.Name", "String=", ""} {
		t.Run(flag, func(t *testing.T) {
			_, err := parseGoTypes([]string{flag})
			assert.ErrorContains(t, err, "--go-type must have format")
		})
	}
}
