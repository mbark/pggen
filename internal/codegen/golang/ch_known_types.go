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
	chString  = gotype.MustParseKnownType("string")
	chDecimal = gotype.MustParseKnownType("github.com/shopspring/decimal.Decimal")
	chTime    = gotype.MustParseKnownType("time.Time")
	chUUID    = gotype.MustParseKnownType("github.com/google/uuid.UUID")
	// ClickHouse hands IP columns back as netip.Addr, both families alike.
	chNetipAddr = gotype.MustParseKnownType("net/netip.Addr")
	// No Go builtin is wide enough for the 128- and 256-bit integers.
	chBigInt = gotype.MustParseKnownType("*math/big.Int")
)

// chScalarTypes maps the parameterless ClickHouse types to Go.
// https://clickhouse.com/docs/sql-reference/data-types
var chScalarTypes = map[string]gotype.Type{
	"String": chString,
	"Bool":   gotype.MustParseKnownType("bool"),

	"Int8":  gotype.MustParseKnownType("int8"),
	"Int16": gotype.MustParseKnownType("int16"),
	"Int32": gotype.MustParseKnownType("int32"),
	"Int64": gotype.MustParseKnownType("int64"),

	"UInt8":  gotype.MustParseKnownType("uint8"),
	"UInt16": gotype.MustParseKnownType("uint16"),
	"UInt32": gotype.MustParseKnownType("uint32"),
	"UInt64": gotype.MustParseKnownType("uint64"),

	"Float32": gotype.MustParseKnownType("float32"),
	"Float64": gotype.MustParseKnownType("float64"),

	"Int128":  chBigInt,
	"Int256":  chBigInt,
	"UInt128": chBigInt,
	"UInt256": chBigInt,

	"Date":       chTime,
	"Date32":     chTime,
	"DateTime":   chTime,
	"DateTime64": chTime,

	"UUID": chUUID,
	"IPv4": chNetipAddr,
	"IPv6": chNetipAddr,
}
