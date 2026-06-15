package golang

import (
	"fmt"
	"github.com/mbark/pggen/internal/casing"
	"github.com/mbark/pggen/internal/codegen"
	"github.com/mbark/pggen/internal/codegen/golang/gotype"
	"github.com/mbark/pggen/internal/gomod"
	"github.com/mbark/pggen/internal/pginfer"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

// Templater creates query file templates.
type Templater struct {
	caser            casing.Caser
	resolver         TypeResolver
	pkg              string // Go package name
	inlineParamCount int
}

// TemplaterOpts is options to control the template logic.
type TemplaterOpts struct {
	Caser    casing.Caser
	Resolver TypeResolver
	Pkg      string // Go package name
	// How many params to inline when calling querier methods.
	InlineParamCount int
}

func NewTemplater(opts TemplaterOpts) Templater {
	return Templater{
		pkg:              opts.Pkg,
		caser:            opts.Caser,
		resolver:         opts.Resolver,
		inlineParamCount: opts.InlineParamCount,
	}
}

// TemplateAll creates query template files for each codegen.QueryFile.
func (tm Templater) TemplateAll(files []codegen.QueryFile) ([]TemplatedFile, error) {
	goQueryFiles := make([]TemplatedFile, 0, len(files))
	allDeclarers := NewDeclarerSet()
	pgTypeNames := make(map[string]struct{})

	// Pick leader file to define common structs and interfaces via Declarer.
	firstIndex := -1
	firstName := string(unicode.MaxRune)
	for i, f := range files {
		if f.SourcePath < firstName {
			firstIndex = i
			firstName = f.SourcePath
		}
	}

	for i, queryFile := range files {
		isLeader := i == firstIndex
		goFile, decls, fileTypeNames, err := tm.templateFile(queryFile, isLeader)
		if err != nil {
			return nil, fmt.Errorf("template query file %s for go: %w", queryFile.SourcePath, err)
		}
		goQueryFiles = append(goQueryFiles, goFile)
		allDeclarers.AddAll(decls.ListAll()...)
		for name := range fileTypeNames {
			pgTypeNames[name] = struct{}{}
		}
	}

	// If there are composite or enum types, add a RegisterTypes declarer.
	if len(pgTypeNames) > 0 {
		names := make([]string, 0, len(pgTypeNames))
		for name := range pgTypeNames {
			names = append(names, name)
		}
		allDeclarers.AddAll(NewTypeRegistrationDeclarer(names))
	}

	// Build shared row struct declarers for queries with output= pragma.
	sharedRowDeclarers, err := tm.buildSharedRowDeclarers(goQueryFiles)
	if err != nil {
		return nil, err
	}
	allDeclarers.AddAll(sharedRowDeclarers...)

	// Add declarers to leader file.
	goQueryFiles[firstIndex].Declarers = allDeclarers.ListAll()

	// Remove unneeded pgconn import if possible.
	for i, file := range goQueryFiles {
		if file.needsPgconnImport() {
			continue
		}
		pgconnIdx := -1
		imports := file.Imports
		for j, imp := range imports {
			if imp.PkgPath == "github.com/jackc/pgx/v5/pgconn" {
				pgconnIdx = j
				break
			}
		}
		if pgconnIdx > -1 {
			copy(imports[pgconnIdx:], imports[pgconnIdx+1:])
			goQueryFiles[i].Imports = imports[:len(imports)-1]
		}
	}

	// Remove self imports.
	for i, file := range goQueryFiles {
		selfPkg, err := gomod.GuessPackage(file.SourcePath)
		if err != nil || selfPkg == "" {
			continue // ignore error, assume it's not a self import
		}
		selfPkgIdx := -1
		imports := file.Imports
		for j, imp := range file.Imports {
			if imp.PkgPath == selfPkg {
				selfPkgIdx = j
				break
			}
		}
		if selfPkgIdx > -1 {
			copy(imports[selfPkgIdx:], imports[selfPkgIdx+1:])
			goQueryFiles[i].Imports = imports[:len(imports)-1]
		}
	}
	return goQueryFiles, nil
}

// templateFile creates the data needed to build a Go file for a query file.
// Also returns any declarations needed by this query file and the set of
// Postgres type names that need registration. The caller must dedupe
// declarations.
func (tm Templater) templateFile(file codegen.QueryFile, isLeader bool) (TemplatedFile, DeclarerSet, map[string]struct{}, error) {
	imports := NewImportSet()
	imports.AddPackage("context")
	imports.AddPackage("fmt")
	imports.AddPackage("github.com/jackc/pgx/v5/pgconn")
	imports.AddPackage("github.com/jackc/pgx/v5")

	pkgPath := ""
	// NOTE: err == nil check
	// Attempt to guess package path. Ignore error if it doesn't work because
	// resolving the package isn't perfect. We'll fall back to an unqualified
	// type which will likely work since the type is probably declared in this
	// package.
	if pkg, err := gomod.GuessPackage(file.SourcePath); err == nil {
		pkgPath = pkg
	}

	// First pass: resolve all types and collect imports.
	type queryData struct {
		query   pginfer.TypedQuery
		doc     string
		inputs  []gotype.Type
		outputs []gotype.Type
	}
	queryDatas := make([]queryData, 0, len(file.Queries))
	declarers := NewDeclarerSet()
	pgTypeNames := make(map[string]struct{})
	for _, query := range file.Queries {
		docs := strings.Builder{}
		avgCharsPerLine := 40
		docs.Grow(len(query.Doc) * avgCharsPerLine)
		for i, d := range query.Doc {
			if i > 0 {
				docs.WriteByte('\t')
			}
			// Avoid a trailing space on empty doc lines.
			if d == "" {
				docs.WriteString("//")
			} else {
				docs.WriteString("// ")
				docs.WriteString(d)
			}
			docs.WriteRune('\n')
		}

		inputTypes := make([]gotype.Type, len(query.Inputs))
		for i, input := range query.Inputs {
			goType, err := tm.resolver.Resolve(input.PgType, false, pkgPath)
			if err != nil {
				return TemplatedFile{}, nil, nil, err
			}
			imports.AddType(goType)
			collectPgTypeNames(goType, pgTypeNames)
			inputTypes[i] = goType
			declarers.AddAll(FindInputDeclarers(goType).ListAll()...)
		}

		outputTypes := make([]gotype.Type, len(query.Outputs))
		for i, out := range query.Outputs {
			goType, err := tm.resolver.Resolve(out.PgType, out.Nullable, pkgPath)
			if err != nil {
				return TemplatedFile{}, nil, nil, err
			}
			imports.AddType(goType)
			collectPgTypeNames(goType, pgTypeNames)
			outputTypes[i] = goType
			declarers.AddAll(FindOutputDeclarers(goType).ListAll()...)
		}

		queryDatas = append(queryDatas, queryData{
			query:   query,
			doc:     docs.String(),
			inputs:  inputTypes,
			outputs: outputTypes,
		})
	}

	// Compute alias map for import collisions.
	aliasMap := imports.AliasMap()

	// Second pass: build templated queries using alias-aware type qualification.
	queries := make([]TemplatedQuery, 0, len(queryDatas))
	for _, qd := range queryDatas {
		inputs := make([]TemplatedParam, len(qd.query.Inputs))
		for i, input := range qd.query.Inputs {
			goType := qd.inputs[i]
			inputs[i] = TemplatedParam{
				UpperName: tm.chooseUpperName(input.PgName, "UnnamedParam", i, len(qd.query.Inputs)),
				LowerName: tm.chooseLowerName(input.PgName, "unnamedParam", i, len(qd.query.Inputs)),
				QualType:  gotype.QualifyType(goType, pkgPath, aliasMap),
				Type:      goType,
				RawName:   qd.query.Inputs[i],
			}
		}

		outputs := make([]TemplatedColumn, len(qd.query.Outputs))
		for i, out := range qd.query.Outputs {
			goType := qd.outputs[i]
			outputs[i] = TemplatedColumn{
				PgName:    out.PgName,
				UpperName: tm.chooseUpperName(out.PgName, "UnnamedColumn", i, len(qd.query.Outputs)),
				LowerName: tm.chooseLowerName(out.PgName, "UnnamedColumn", i, len(qd.query.Outputs)),
				Type:      goType,
				QualType:  gotype.QualifyType(goType, pkgPath, aliasMap),
			}
		}

		tq := TemplatedQuery{
			Name:             tm.caser.ToUpperGoIdent(qd.query.Name),
			SQLVarName:       tm.caser.ToLowerGoIdent(qd.query.Name) + "SQL",
			ResultKind:       qd.query.ResultKind,
			Doc:              qd.doc,
			PreparedSQL:      qd.query.PreparedSQL,
			Inputs:           inputs,
			Outputs:          outputs,
			InlineParamCount: tm.inlineParamCount,
			OutputType:       qd.query.OutputType,
			VariantGroup:     qd.query.VariantGroup,
			VariantKey:       qd.query.VariantKey,
		}
		if tq.IsVariant() {
			// Name the SQL const after the unexported helper so the generated
			// code reads cleanly and stays unique per variant.
			tq.SQLVarName = tq.VariantMethodName() + "SQL"
		}
		queries = append(queries, tq)
	}

	// Partition fanned-out variants from regular queries and build dispatcher
	// groups in deterministic (first-seen) order.
	var regular []TemplatedQuery
	var variants []TemplatedQuery
	var groupOrder []string
	byGroup := make(map[string][]TemplatedQuery)
	for _, q := range queries {
		if !q.IsVariant() {
			regular = append(regular, q)
			continue
		}
		if _, ok := byGroup[q.VariantGroup]; !ok {
			groupOrder = append(groupOrder, q.VariantGroup)
		}
		byGroup[q.VariantGroup] = append(byGroup[q.VariantGroup], q)
		variants = append(variants, q)
	}
	var groups []PaginateGroup
	for _, name := range groupOrder {
		groups = append(groups, buildPaginateGroup(name, byGroup[name]))
	}

	return TemplatedFile{
		PkgPath:        pkgPath,
		GoPkg:          tm.pkg,
		SourcePath:     file.SourcePath,
		Queries:        regular,
		Variants:       variants,
		PaginateGroups: groups,
		Imports:        imports.SortedImports(),
		RawImports:     imports.SortedPackages(),
		IsLeader:       isLeader,
	}, declarers, pgTypeNames, nil
}

// buildPaginateGroup assembles the dispatcher metadata for a set of variants:
// the union of their inputs (by Postgres column name) and the distinct
// sort-key constants.
func buildPaginateGroup(name string, variants []TemplatedQuery) PaginateGroup {
	var unified []TemplatedParam
	seen := make(map[string]bool)
	for _, v := range variants {
		for _, in := range v.Inputs {
			if seen[in.RawName.PgName] {
				continue
			}
			seen[in.RawName.PgName] = true
			unified = append(unified, in)
		}
	}
	var consts []SortKeyConst
	seenKey := make(map[string]bool)
	for _, v := range variants {
		if v.VariantKey.IsDefault || seenKey[v.VariantKey.SortKey] {
			continue
		}
		seenKey[v.VariantKey.SortKey] = true
		consts = append(consts, SortKeyConst{
			ConstName: name + "Sort" + snakeToCamel(v.VariantKey.SortKey),
			Value:     v.VariantKey.SortKey,
		})
	}
	return PaginateGroup{
		Name:           name,
		ParamsTypeName: name + "Params",
		UnifiedInputs:  unified,
		SortKeyConsts:  consts,
		Variants:       variants,
	}
}

// chooseUpperName converts pgName into a capitalized Go identifier name.
// If it's not possible to convert pgName into an identifier, uses fallback with
// a suffix using idx.
func (tm Templater) chooseUpperName(pgName string, fallback string, idx int, numOptions int) string {
	if name := tm.caser.ToUpperGoIdent(pgName); name != "" {
		return name
	}
	suffix := strconv.Itoa(idx)
	if numOptions > 9 {
		suffix = fmt.Sprintf("%2d", idx)
	}
	return fallback + suffix
}

// chooseLowerName converts pgName into an uncapitalized Go identifier name.
// If it's not possible to convert pgName into an identifier, uses fallback with
// a suffix using idx.
func (tm Templater) chooseLowerName(pgName string, fallback string, idx int, numOptions int) string {
	if name := tm.caser.ToLowerGoIdent(pgName); name != "" {
		return name
	}
	suffix := strconv.Itoa(idx)
	if numOptions > 9 {
		suffix = fmt.Sprintf("%2d", idx)
	}
	return fallback + suffix
}

// buildSharedRowDeclarers collects all queries with output= pragma, validates
// that queries sharing the same output type have compatible columns, merges
// nullability (widening to pointer types), and returns Declarers for the shared
// row structs.
func (tm Templater) buildSharedRowDeclarers(files []TemplatedFile) ([]Declarer, error) {
	// Collect queries grouped by OutputType.
	type queryRef struct {
		fileName  string
		queryName string
		outputs   []TemplatedColumn
	}
	groups := make(map[string][]queryRef)
	// Process files and queries in deterministic order (files are already sorted).
	for _, f := range files {
		queries := append(append([]TemplatedQuery{}, f.Queries...), f.Variants...)
		for _, q := range queries {
			if q.OutputType == "" {
				continue
			}
			groups[q.OutputType] = append(groups[q.OutputType], queryRef{
				fileName:  f.SourcePath,
				queryName: q.Name,
				outputs:   removeVoidColumns(q.Outputs),
			})
		}
	}

	if len(groups) == 0 {
		return nil, nil
	}

	// Sort output type names for deterministic processing.
	outputTypeNames := make([]string, 0, len(groups))
	for name := range groups {
		outputTypeNames = append(outputTypeNames, name)
	}
	sort.Strings(outputTypeNames)

	var declarers []Declarer
	for _, outputType := range outputTypeNames {
		refs := groups[outputType]

		// Use the first query's column order as the canonical order.
		first := refs[0]
		if len(first.outputs) == 0 {
			return nil, fmt.Errorf("output type %q on query %s has no output columns", outputType, first.queryName)
		}

		// Build a map from PgName -> column for the first query.
		type mergedCol struct {
			col      TemplatedColumn
			nullable bool // track if any query has this column as nullable
		}
		colByName := make(map[string]*mergedCol, len(first.outputs))
		colOrder := make([]string, len(first.outputs))
		for i, col := range first.outputs {
			_, isPtr := col.Type.(*gotype.PointerType)
			colByName[col.PgName] = &mergedCol{col: col, nullable: isPtr}
			colOrder[i] = col.PgName
		}

		// Validate and merge subsequent queries.
		for _, ref := range refs[1:] {
			if len(ref.outputs) != len(first.outputs) {
				return nil, fmt.Errorf(
					"output type %q: query %s has %d columns but query %s has %d columns",
					outputType, first.queryName, len(first.outputs), ref.queryName, len(ref.outputs),
				)
			}
			for _, col := range ref.outputs {
				mc, ok := colByName[col.PgName]
				if !ok {
					// Collect the column names from the first query for the error.
					firstCols := make([]string, len(first.outputs))
					for i, c := range first.outputs {
						firstCols[i] = c.PgName
					}
					refCols := make([]string, len(ref.outputs))
					for i, c := range ref.outputs {
						refCols[i] = c.PgName
					}
					return nil, fmt.Errorf(
						"output type %q: query %s has column %q not present in query %s; "+
							"%s columns: %v, %s columns: %v",
						outputType, ref.queryName, col.PgName, first.queryName,
						first.queryName, firstCols, ref.queryName, refCols,
					)
				}

				// Check type compatibility: base types must match.
				baseExisting := unwrapPointer(mc.col.Type)
				baseNew := unwrapPointer(col.Type)
				if baseExisting.BaseName() != baseNew.BaseName() {
					return nil, fmt.Errorf(
						"output type %q: column %q has incompatible types: %s in query %s vs %s in query %s",
						outputType, col.PgName,
						mc.col.QualType, first.queryName,
						col.QualType, ref.queryName,
					)
				}

				// Widen nullability: if the new column is a pointer, mark as nullable.
				if _, isPtr := col.Type.(*gotype.PointerType); isPtr {
					mc.nullable = true
				}
			}
		}

		// Build the final merged columns in the canonical order.
		merged := make([]TemplatedColumn, len(colOrder))
		for i, pgName := range colOrder {
			mc := colByName[pgName]
			col := mc.col
			// If any query had this column as nullable but the first didn't,
			// widen to pointer type.
			if mc.nullable {
				if _, isPtr := col.Type.(*gotype.PointerType); !isPtr {
					col.Type = &gotype.PointerType{Elem: col.Type}
					col.QualType = "*" + col.QualType
				}
			}
			merged[i] = col
		}

		declarers = append(declarers, NewSharedRowDeclarer(outputType, merged))
	}

	return declarers, nil
}

// unwrapPointer returns the underlying type if t is a PointerType, otherwise t.
func unwrapPointer(t gotype.Type) gotype.Type {
	if pt, ok := t.(*gotype.PointerType); ok {
		return pt.Elem
	}
	return t
}
