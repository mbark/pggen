// Package sqltype defines the database type contract shared by every pggen
// dialect. It exists so that the query representation and the Go code
// generator can talk about types without depending on a particular database.
//
// Each dialect defines its own concrete types implementing Type:
// internal/pg models Postgres types identified by OID, and internal/ch models
// ClickHouse types identified by their canonical name.
package sqltype

// Type is a database type.
type Type interface {
	// String returns the name of the type as the database spells it, like
	// "int8" in Postgres or "Array(String)" in ClickHouse.
	String() string
	// Key returns a stable identity for this type, unique across dialects. It
	// keys the per-dialect known-type tables and dedupes generated
	// declarations, so it must distinguish two types that share a name but
	// mean different things.
	Key() string
}

// ArrayType is implemented by a dialect Type that represents an array of some
// element type, like Postgres _int4 or ClickHouse Array(Int32). The Go code
// generator uses it to check that a Go slice type is backed by an array in the
// database.
type ArrayType interface {
	Type
	ElemType() Type
}
