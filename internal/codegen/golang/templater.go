package golang

import (
	"fmt"
	"github.com/mbark/pggen/internal/casing"
	"github.com/mbark/pggen/internal/codegen"
	"github.com/mbark/pggen/internal/codegen/golang/gotype"
	"github.com/mbark/pggen/internal/gomod"
	"github.com/mbark/pggen/internal/pginfer"
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
			docs.WriteString("// ")
			docs.WriteString(d)
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

		queries = append(queries, TemplatedQuery{
			Name:             tm.caser.ToUpperGoIdent(qd.query.Name),
			SQLVarName:       tm.caser.ToLowerGoIdent(qd.query.Name) + "SQL",
			ResultKind:       qd.query.ResultKind,
			Doc:              qd.doc,
			PreparedSQL:      qd.query.PreparedSQL,
			Inputs:           inputs,
			Outputs:          outputs,
			InlineParamCount: tm.inlineParamCount,
		})
	}

	return TemplatedFile{
		PkgPath:    pkgPath,
		GoPkg:      tm.pkg,
		SourcePath: file.SourcePath,
		Queries:    queries,
		Imports:    imports.SortedImports(),
		RawImports: imports.SortedPackages(),
		IsLeader:   isLeader,
	}, declarers, pgTypeNames, nil
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
