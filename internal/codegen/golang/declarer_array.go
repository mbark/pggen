package golang

import (
	"github.com/mbark/pggen/internal/codegen/golang/gotype"
)

// In pgx v5, arrays of simple types are handled natively. For arrays of
// composite or enum types, the user must register the type with the connection.
// pggen no longer generates array transcoder functions.

// NameArrayInitFunc returns the name for a hypothetical array init function.
// Kept for backward compat in name generation only.
func NameArrayInitFunc(typ *gotype.ArrayType) string {
	elem := typ.Elem
	if t, ok := elem.(*gotype.ImportType); ok {
		elem = t.Type
	}
	hasPtr := false
	if t, ok := elem.(*gotype.PointerType); ok {
		hasPtr = true
		elem = t.Elem
	}
	if hasPtr {
		return "new" + elem.BaseName() + "PtrArrayInit"
	} else {
		return "new" + elem.BaseName() + "ArrayInit"
	}
}
