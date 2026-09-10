package ch

import (
	"strings"
	"testing"
)

func TestZeroLiteral(t *testing.T) {
	tests := []struct{ src, want string }{
		{src: "String", want: ""},
		{src: "FixedString(16)", want: ""},
		{src: "Int64", want: "0"},
		{src: "UInt32", want: "0"},
		{src: "Float64", want: "0"},
		{src: "Bool", want: "false"},
		{src: "Date", want: "1970-01-01"},
		{src: "DateTime", want: "1970-01-01 00:00:00"},
		{src: "DateTime('UTC')", want: "1970-01-01 00:00:00"},
		{src: "DateTime64(3, 'UTC')", want: "1970-01-01 00:00:00"},
		{src: "Decimal(18, 6)", want: "0"},
		{src: "UUID", want: "00000000-0000-0000-0000-000000000000"},
		{src: "IPv4", want: "0.0.0.0"},
		{src: "IPv6", want: "::"},
		{src: "Array(String)", want: "[]"},
		{src: "Map(String, String)", want: "{}"},
		// Wrappers delegate to the type they wrap.
		{src: "Nullable(Int64)", want: "0"},
		{src: "LowCardinality(String)", want: ""},
		{src: "LowCardinality(Nullable(String))", want: ""},
		// Any declared enum label parses; the first always exists.
		{src: "Enum8('MOC' = 1, 'GPRS' = 7)", want: "MOC"},
		{src: "Tuple(a UInt8, b String)", want: "(0,)"},
	}
	for _, tt := range tests {
		t.Run(tt.src, func(t *testing.T) {
			got, err := ZeroLiteral(MustParse(tt.src))
			if err != nil {
				t.Fatalf("ZeroLiteral(%q) returned error: %v", tt.src, err)
			}
			if got != tt.want {
				t.Errorf("ZeroLiteral(%q) = %q; want %q", tt.src, got, tt.want)
			}
		})
	}
}

func TestZeroLiteral_errors(t *testing.T) {
	tests := []struct {
		name    string
		src     string
		wantSub string
	}{
		{
			// The one parameter kind inference genuinely cannot handle.
			name:    "identifier",
			src:     "Identifier",
			wantSub: "Identifier",
		},
		{
			name:    "unsupported_type",
			src:     "AggregateFunction(sum, UInt64)",
			wantSub: "AggregateFunction",
		},
		{
			name:    "unknown_scalar",
			src:     "SomeFutureType",
			wantSub: "SomeFutureType",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ZeroLiteral(MustParse(tt.src))
			if err == nil {
				t.Fatalf("ZeroLiteral(%q) succeeded; want an error", tt.src)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("ZeroLiteral(%q) error = %q; want it to name %q", tt.src, err, tt.wantSub)
			}
		})
	}
}
