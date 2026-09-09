package golang

import (
	"github.com/mbark/pggen/internal/ch"
	"github.com/mbark/pggen/internal/codegen/golang/gotype"
)

// findChKnownType returns the Go type for a leaf ClickHouse type — everything
// that isn't a wrapper like Nullable, Array or Map, which the resolver handles
// structurally.
//
// Unlike the Postgres table, there is no nullable/non-nullable pair: ClickHouse
// spells nullability as Nullable(T), which the resolver turns into a pointer,
// so each type appears once.
func findChKnownType(t ch.Type) (gotype.Type, bool) {
	switch t := t.(type) {
	case ch.FixedString:
		return chString, true
	case ch.Decimal:
		return chDecimal, true
	case ch.DateTime, ch.DateTime64:
		return chTime, true
	case ch.Scalar:
		typ, ok := chScalarTypes[t.Name]
		return typ, ok
	}
	return nil, false
}

var (
	chString  = gotype.MustParseKnownType("string", ch.Scalar{Name: "String"})
	chDecimal = gotype.MustParseKnownType(
		"github.com/shopspring/decimal.Decimal", ch.Decimal{Precision: 18, Scale: 6})
	chTime = gotype.MustParseKnownType("time.Time", ch.DateTime{})
	chUUID = gotype.MustParseKnownType(
		"github.com/google/uuid.UUID", ch.Scalar{Name: "UUID"})
	// ClickHouse hands IP columns back as netip.Addr.
	chIPv4 = gotype.MustParseKnownType("net/netip.Addr", ch.Scalar{Name: "IPv4"})
	chIPv6 = gotype.MustParseKnownType("net/netip.Addr", ch.Scalar{Name: "IPv6"})
)

// chScalarTypes maps the parameterless ClickHouse types to Go.
// https://clickhouse.com/docs/sql-reference/data-types
var chScalarTypes = map[string]gotype.Type{
	"String": chString,
	"Bool":   gotype.MustParseKnownType("bool", ch.Scalar{Name: "Bool"}),

	"Int8":  gotype.MustParseKnownType("int8", ch.Scalar{Name: "Int8"}),
	"Int16": gotype.MustParseKnownType("int16", ch.Scalar{Name: "Int16"}),
	"Int32": gotype.MustParseKnownType("int32", ch.Scalar{Name: "Int32"}),
	"Int64": gotype.MustParseKnownType("int64", ch.Scalar{Name: "Int64"}),

	"UInt8":  gotype.MustParseKnownType("uint8", ch.Scalar{Name: "UInt8"}),
	"UInt16": gotype.MustParseKnownType("uint16", ch.Scalar{Name: "UInt16"}),
	"UInt32": gotype.MustParseKnownType("uint32", ch.Scalar{Name: "UInt32"}),
	"UInt64": gotype.MustParseKnownType("uint64", ch.Scalar{Name: "UInt64"}),

	"Float32": gotype.MustParseKnownType("float32", ch.Scalar{Name: "Float32"}),
	"Float64": gotype.MustParseKnownType("float64", ch.Scalar{Name: "Float64"}),

	// The 128- and 256-bit integers arrive as big.Int; there is no Go builtin
	// wide enough.
	"Int128":  gotype.MustParseKnownType("*math/big.Int", ch.Scalar{Name: "Int128"}),
	"Int256":  gotype.MustParseKnownType("*math/big.Int", ch.Scalar{Name: "Int256"}),
	"UInt128": gotype.MustParseKnownType("*math/big.Int", ch.Scalar{Name: "UInt128"}),
	"UInt256": gotype.MustParseKnownType("*math/big.Int", ch.Scalar{Name: "UInt256"}),

	"Date":       chTime,
	"Date32":     chTime,
	"DateTime":   chTime,
	"DateTime64": chTime,

	"UUID": chUUID,
	"IPv4": chIPv4,
	"IPv6": chIPv6,
}
