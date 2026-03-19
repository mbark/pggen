package gotype

import (
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mbark/pggen/internal/pg"
	"github.com/mbark/pggen/internal/pg/pgoid"
)

// FindKnownTypePgx returns the native pgx type, like pgtype.Text, if known, for
// a Postgres OID. If there is no known type, returns nil.
func FindKnownTypePgx(oid uint32) (Type, bool) {
	typ, ok := knownTypesByOID[oid]
	return typ.pgNative, ok
}

// FindKnownTypeNullable returns the nullable type, like *string, if known, for
// a Postgres OID. Falls back to the pgNative type. If there is no known type
// for the OID, returns nil.
func FindKnownTypeNullable(oid uint32) (Type, bool) {
	typ, ok := knownTypesByOID[oid]
	if !ok {
		return nil, false
	}
	if typ.nullable != nil {
		return typ.nullable, true
	}
	return typ.pgNative, true
}

// FindKnownTypeNonNullable returns the non-nullable type like string, if known,
// for a Postgres OID. Falls back to the nullable type and pgNative type. If
// there is no known type for the OID, returns nil.
func FindKnownTypeNonNullable(oid uint32) (Type, bool) {
	typ, ok := knownTypesByOID[oid]
	if !ok {
		return nil, false
	}
	if typ.nonNullable != nil {
		return typ.nonNullable, true
	}
	if typ.nullable != nil {
		return typ.nullable, true
	}
	return typ.pgNative, true
}

// Native go types are not prefixed.
//
//goland:noinspection GoUnusedGlobalVariable
var (
	Bool          = MustParseKnownType("bool", pg.Bool)
	Boolp         = MustParseKnownType("*bool", pg.Bool)
	BoolSlice     = MustParseKnownType("[]bool", pg.BoolArray)
	Int           = MustParseKnownType("int", pg.Int8)
	Intp          = MustParseKnownType("*int", pg.Int8)
	IntSlice      = MustParseKnownType("[]int", pg.Int8Array)
	IntpSlice     = MustParseKnownType("[]*int", pg.Int8Array)
	Int16         = MustParseKnownType("int16", pg.Int2)
	Int16p        = MustParseKnownType("*int16", pg.Int2)
	Int16Slice    = MustParseKnownType("[]int16", pg.Int2Array)
	Int16pSlice   = MustParseKnownType("[]*int16", pg.Int2Array)
	Int32         = MustParseKnownType("int32", pg.Int4)
	Int32p        = MustParseKnownType("*int32", pg.Int4)
	Int32Slice    = MustParseKnownType("[]int32", pg.Int4Array)
	Int32pSlice   = MustParseKnownType("[]*int32", pg.Int4Array)
	Int64         = MustParseKnownType("int64", pg.Int8)
	Int64p        = MustParseKnownType("*int64", pg.Int8)
	Int64Slice    = MustParseKnownType("[]int64", pg.Int8Array)
	Int64pSlice   = MustParseKnownType("[]*int64", pg.Int8Array)
	Uint          = MustParseKnownType("uint", pg.Int8)
	UintSlice     = MustParseKnownType("[]uint", pg.Int8Array)
	Uint16        = MustParseKnownType("uint16", pg.Int2)
	Uint16Slice   = MustParseKnownType("[]uint16", pg.Int2Array)
	Uint32        = MustParseKnownType("uint32", pg.Int4)
	Uint32Slice   = MustParseKnownType("[]uint32", pg.Int4Array)
	Uint64        = MustParseKnownType("uint64", pg.Int8)
	Uint64Slice   = MustParseKnownType("[]uint64", pg.Int8Array)
	String        = MustParseKnownType("string", pg.Text)
	Stringp       = MustParseKnownType("*string", pg.Text)
	StringSlice   = MustParseKnownType("[]string", pg.TextArray)
	StringpSlice  = MustParseKnownType("[]*string", pg.TextArray)
	Float32       = MustParseKnownType("float32", pg.Float4)
	Float32p      = MustParseKnownType("*float32", pg.Float4)
	Float32Slice  = MustParseKnownType("[]float32", pg.Float4Array)
	Float32pSlice = MustParseKnownType("[]*float32", pg.Float4Array)
	Float64       = MustParseKnownType("float64", pg.Float8)
	Float64p      = MustParseKnownType("*float64", pg.Float8)
	Float64Slice  = MustParseKnownType("[]float64", pg.Float8Array)
	Float64pSlice = MustParseKnownType("[]*float64", pg.Float8Array)
	ByteSlice     = MustParseKnownType("[]byte", pg.Bytea)
)

// pgtype types prefixed with "pg".
var (
	PgBool             = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Bool", pg.Bool)
	PgInt8             = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Int8", pg.Int8)
	PgInt2             = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Int2", pg.Int2)
	PgInt4             = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Int4", pg.Int4)
	PgText             = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Text", pg.Text)
	PgJSON             = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.JSONBCodec", pg.JSON)
	PgPoint            = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Point", pg.Point)
	PgLseg             = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Lseg", pg.Lseg)
	PgPath             = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Path", pg.Path)
	PgBox              = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Box", pg.Box)
	PgPolygon          = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Polygon", pg.Polygon)
	PgLine             = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Line", pg.Line)
	PgCIDR             = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Bits", pg.CIDR)
	PgFloat4           = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Float4", pg.Float4)
	PgFloat8           = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Float8", pg.Float8)
	PgCircle           = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Circle", pg.Circle)
	PgInet             = MustParseKnownType("net/netip.Prefix", pg.Inet)
	PgMacaddr          = MustParseKnownType("net.HardwareAddr", pg.Macaddr)
	PgDate             = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Date", pg.Date)
	PgTime             = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Time", pg.Time)
	PgTimestamp        = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Timestamp", pg.Timestamp)
	PgTimestamptz      = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Timestamptz", pg.Timestamptz)
	PgInterval         = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Interval", pg.Interval)
	PgBit              = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Bits", pg.Bit)
	PgVarbit           = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Bits", pg.Varbit)
	PgVoid             = &VoidType{}
	PgNumeric          = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Numeric", pg.Numeric)
	PgUUID             = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.UUID", pg.UUID)
	PgJSONB            = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.JSONB", pg.JSONB)
	PgInt4range        = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Range[github.com/jackc/pgx/v5/pgtype.Int4]", pg.Int4range)
	PgNumrange         = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Range[github.com/jackc/pgx/v5/pgtype.Numeric]", pg.Numrange)
	PgTsrange          = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Range[github.com/jackc/pgx/v5/pgtype.Timestamp]", pg.Tsrange)
	PgTstzrange        = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Range[github.com/jackc/pgx/v5/pgtype.Timestamptz]", pg.Tstzrange)
	PgDaterange        = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Range[github.com/jackc/pgx/v5/pgtype.Date]", pg.Daterange)
	PgInt8range        = MustParseKnownType("github.com/jackc/pgx/v5/pgtype.Range[github.com/jackc/pgx/v5/pgtype.Int8]", pg.Int8range)
)

// knownGoType is the native pgtype type, the nullable and non-nullable types
// for a Postgres type.
//
// pgNative means a type that implements the pgx decoder methods directly.
// Such types are typically provided by the pgtype package. Used as the fallback
// type and for cases like composite types where we need a scanner type.
//
// A nullable type is one that can represent a nullable column, like *string for
// a Postgres text type that can be null. A nullable type is nicer to work with
// than the corresponding pgNative type, i.e. "*string" is easier to work with
// than pgtype.Text{}.
//
// A nonNullable type is one that can represent a column that's never null, like
// "string" for a Postgres text type.
type knownGoType struct{ pgNative, nullable, nonNullable Type }

var knownTypesByOID = map[uint32]knownGoType{
	pgtype.BoolOID:             {PgBool, Boolp, Bool},
	pgtype.QCharOID:            {Int32, Int32p, Int32},
	pgtype.NameOID:             {String, Stringp, String},
	pgtype.Int8OID:             {PgInt8, Intp, Int},
	pgtype.Int2OID:             {PgInt2, Int16p, Int16},
	pgtype.Int4OID:             {PgInt4, Int32p, Int32},
	pgtype.TextOID:             {PgText, Stringp, String},
	pgtype.ByteaOID:            {ByteSlice, ByteSlice, ByteSlice},
	pgtype.OIDOID:              {Uint32, nil, nil},
	pgtype.TIDOID:              {Uint32, nil, nil},
	pgtype.XIDOID:              {Uint32, nil, nil},
	pgtype.CIDOID:              {Uint32, nil, nil},
	pgtype.JSONOID:             {ByteSlice, ByteSlice, ByteSlice},
	pgtype.PointOID:            {PgPoint, nil, nil},
	pgtype.LsegOID:             {PgLseg, nil, nil},
	pgtype.PathOID:             {PgPath, nil, nil},
	pgtype.BoxOID:              {PgBox, nil, nil},
	pgtype.PolygonOID:          {PgPolygon, nil, nil},
	pgtype.LineOID:             {PgLine, nil, nil},
	pgtype.CIDROID:             {String, nil, nil},
	pgtype.CIDRArrayOID:        {StringSlice, nil, nil},
	pgtype.Float4OID:           {PgFloat4, Float32p, Float32},
	pgtype.Float8OID:           {PgFloat8, Float64p, Float64},
	pgoid.OIDArray:             {Uint32Slice, nil, nil},
	pgtype.UnknownOID:          {String, nil, nil},
	pgtype.CircleOID:           {PgCircle, nil, nil},
	pgtype.MacaddrOID:          {PgMacaddr, nil, nil},
	pgtype.InetOID:             {PgInet, nil, nil},
	pgtype.BoolArrayOID:        {BoolSlice, nil, nil},
	pgtype.ByteaArrayOID:       {StringSlice, nil, nil},
	pgtype.Int2ArrayOID:        {Int16Slice, Int16pSlice, Int16Slice},
	pgtype.Int4ArrayOID:        {Int32Slice, Int32pSlice, Int32Slice},
	pgtype.TextArrayOID:        {StringSlice, StringSlice, nil},
	pgtype.BPCharArrayOID:      {StringSlice, nil, nil},
	pgtype.VarcharArrayOID:     {StringSlice, nil, nil},
	pgtype.Int8ArrayOID:        {IntSlice, IntpSlice, IntSlice},
	pgtype.Float4ArrayOID:      {Float32Slice, Float32pSlice, Float32Slice},
	pgtype.Float8ArrayOID:      {Float64Slice, Float64pSlice, Float64Slice},
	pgtype.ACLItemOID:          {String, nil, nil},
	pgtype.ACLItemArrayOID:     {StringSlice, nil, nil},
	pgtype.InetArrayOID:        {StringSlice, nil, nil},
	pgoid.MacaddrArray:         {StringSlice, nil, nil},
	pgtype.BPCharOID:           {String, Stringp, String},
	pgtype.VarcharOID:          {String, Stringp, String},
	pgtype.DateOID:             {PgDate, nil, nil},
	pgtype.TimeOID:             {PgTime, nil, nil},
	pgtype.TimestampOID:        {PgTimestamp, nil, nil},
	pgtype.TimestampArrayOID:   {StringSlice, nil, nil},
	pgtype.DateArrayOID:        {StringSlice, nil, nil},
	pgtype.TimestamptzOID:      {PgTimestamptz, nil, nil},
	pgtype.TimestamptzArrayOID: {StringSlice, nil, nil},
	pgtype.IntervalOID:         {PgInterval, nil, nil},
	pgtype.NumericArrayOID:     {StringSlice, nil, nil},
	pgtype.BitOID:              {PgBit, nil, nil},
	pgtype.VarbitOID:           {PgVarbit, nil, nil},
	pgoid.Void:                 {PgVoid, nil, nil},
	pgtype.NumericOID:          {PgNumeric, nil, nil},
	pgtype.RecordOID:           {nil, nil, nil},
	pgtype.UUIDOID:             {PgUUID, nil, nil},
	pgtype.UUIDArrayOID:        {StringSlice, nil, nil},
	pgtype.JSONBOID:            {ByteSlice, ByteSlice, ByteSlice},
	pgtype.JSONBArrayOID:       {StringSlice, nil, nil},
	pgtype.Int4rangeOID:        {PgInt4range, nil, nil},
	pgtype.NumrangeOID:         {PgNumrange, nil, nil},
	pgtype.TsrangeOID:          {PgTsrange, nil, nil},
	pgtype.TstzrangeOID:        {PgTstzrange, nil, nil},
	pgtype.DaterangeOID:        {PgDaterange, nil, nil},
	pgtype.Int8rangeOID:        {PgInt8range, nil, nil},
}
