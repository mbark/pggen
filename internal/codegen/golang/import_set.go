package golang

import (
	"github.com/mbark/pggen/internal/codegen/golang/gotype"
	"sort"
	"strings"
)

// ImportSet contains a set of imports required by one Go file.
type ImportSet struct {
	imports map[string]struct{}
}

func NewImportSet() *ImportSet {
	return &ImportSet{imports: make(map[string]struct{}, 4)}
}

// AddPackage adds a fully qualified package path to the set, like
// "github.com/mbark/pggen/foo".
func (s *ImportSet) AddPackage(p string) {
	s.imports[p] = struct{}{}
}

// AddType adds all fully qualified package paths needed for type and any child
// types.
func (s *ImportSet) AddType(typ gotype.Type) {
	s.AddPackage(typ.Import())
	unwrapped := gotype.UnwrapNestedType(typ)
	comp, ok := unwrapped.(*gotype.CompositeType)
	if !ok {
		return
	}
	for _, childType := range comp.FieldTypes {
		s.AddType(childType)
	}
}

// ImportPkg is a single import entry, optionally with an alias.
type ImportPkg struct {
	PkgPath string // e.g. "github.com/jackc/pgtype"
	Alias   string // e.g. "pgtype4", or "" for no alias
}

// FormatImport returns the Go import line content, e.g. `"pkg"` or `alias "pkg"`.
func (ip ImportPkg) FormatImport() string {
	if ip.Alias != "" {
		return ip.Alias + ` "` + ip.PkgPath + `"`
	}
	return `"` + ip.PkgPath + `"`
}

// SortedImports returns import entries with aliases assigned where needed to
// resolve short-name collisions, sorted by package path.
func (s *ImportSet) SortedImports() []ImportPkg {
	pkgs := s.SortedPackages()
	// Build short-name -> list of full paths.
	byShort := make(map[string][]string)
	for _, pkg := range pkgs {
		short := gotype.ExtractShortPackage([]byte(pkg))
		byShort[short] = append(byShort[short], pkg)
	}
	// For collisions, assign aliases to all but the first (alphabetically).
	aliases := make(map[string]string) // full path -> alias
	for short, paths := range byShort {
		if len(paths) < 2 {
			continue
		}
		// paths are already sorted (from SortedPackages). First keeps the
		// bare name, rest get numeric suffixes.
		for i := 1; i < len(paths); i++ {
			aliases[paths[i]] = short + strings.Repeat("_", i)
		}
	}
	result := make([]ImportPkg, len(pkgs))
	for i, pkg := range pkgs {
		result[i] = ImportPkg{PkgPath: pkg, Alias: aliases[pkg]}
	}
	return result
}

// AliasMap returns a map from full package path to the alias that should be
// used when qualifying types. Returns nil if there are no collisions.
func (s *ImportSet) AliasMap() map[string]string {
	imports := s.SortedImports()
	var m map[string]string
	for _, imp := range imports {
		if imp.Alias != "" {
			if m == nil {
				m = make(map[string]string)
			}
			m[imp.PkgPath] = imp.Alias
		}
	}
	return m
}

// SortedPackages returns a new slice containing the sorted packages, suitable
// for an import statement.
func (s *ImportSet) SortedPackages() []string {
	imps := make([]string, 0, len(s.imports))
	for pkg := range s.imports {
		if pkg != "" {
			imps = append(imps, pkg)
		}
	}
	sort.Strings(imps)
	return imps
}
