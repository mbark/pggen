package parser

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mbark/pggen/internal/ast"
	gotok "go/token"
)

// collectSortSpecs scans every comment group for a "sort: <name>" block and
// parses it into an ast.SortSpec. A spec block is a single contiguous comment
// group, e.g.:
//
//	-- sort: payments_sort
//	--   key payment_date: payment_date, payment_id
//	--   nullable: payment_date
//	--   default: payment_id
//	--   cursor: payment_date=cursor_payment_date, payment_id=cursor_payment_id
func collectSortSpecs(comments []*ast.CommentGroup) (map[string]*ast.SortSpec, error) {
	specs := make(map[string]*ast.SortSpec)
	for _, g := range comments {
		lines := make([]string, 0, len(g.List))
		for _, c := range g.List {
			lines = append(lines, stripComment(c.Text))
		}
		name, ok := sortBlockName(lines)
		if !ok {
			continue
		}
		spec, err := parseSortSpec(name, lines)
		if err != nil {
			return nil, err
		}
		if _, dup := specs[name]; dup {
			return nil, fmt.Errorf("duplicate sort spec %q", name)
		}
		specs[name] = spec
	}
	return specs, nil
}

func stripComment(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimLeft(text, "-")
	return strings.TrimSpace(text)
}

func sortBlockName(lines []string) (string, bool) {
	for _, l := range lines {
		if rest, ok := strings.CutPrefix(l, "sort:"); ok {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

func parseSortSpec(name string, lines []string) (*ast.SortSpec, error) {
	spec := &ast.SortSpec{Name: name, Cursor: map[string]string{}}
	var nullable []string
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "sort:"):
			// already handled
		case strings.HasPrefix(l, "key "):
			rest := strings.TrimPrefix(l, "key ")
			keyName, cols, ok := strings.Cut(rest, ":")
			if !ok {
				return nil, fmt.Errorf("sort spec %q: malformed key line %q (want 'key <name>: col, col')", name, l)
			}
			spec.Keys = append(spec.Keys, ast.SortKey{
				Name:    strings.TrimSpace(keyName),
				Columns: splitTerms(cols),
			})
		case strings.HasPrefix(l, "nullable:"):
			nullable = append(nullable, splitTerms(strings.TrimPrefix(l, "nullable:"))...)
		case strings.HasPrefix(l, "tiebreak:"):
			spec.Tiebreak = strings.TrimSpace(strings.TrimPrefix(l, "tiebreak:"))
		case strings.HasPrefix(l, "default:"):
			spec.DefaultBy = splitTerms(strings.TrimPrefix(l, "default:"))
		case strings.HasPrefix(l, "cursor:"):
			for _, pair := range splitList(strings.TrimPrefix(l, "cursor:")) {
				col, arg, ok := strings.Cut(pair, "=")
				if !ok {
					return nil, fmt.Errorf("sort spec %q: malformed cursor binding %q (want 'col=arg')", name, pair)
				}
				spec.Cursor[strings.TrimSpace(col)] = strings.TrimSpace(arg)
			}
		}
	}

	if len(spec.Keys) == 0 {
		return nil, fmt.Errorf("sort spec %q: no key declared", name)
	}
	if len(spec.DefaultBy) == 0 {
		return nil, fmt.Errorf("sort spec %q: no default ordering declared", name)
	}
	for i := range spec.Keys {
		for _, col := range spec.Keys[i].Columns {
			if contains(nullable, col) {
				spec.Keys[i].Nullable = append(spec.Keys[i].Nullable, col)
			}
		}
		// The leading-only-nullable restriction exists for the keyset predicate;
		// ordering-only specs (no cursor) may have any nullable columns.
		if spec.IsKeyset() {
			if err := validateNullable(name, spec.Keys[i]); err != nil {
				return nil, err
			}
		}
	}
	if spec.IsKeyset() {
		if err := validateCursorBindings(spec); err != nil {
			return nil, err
		}
	}
	return spec, nil
}

// validateNullable restricts the generated keyset predicate to the supported
// shape: at most the leading column may be nullable.
func validateNullable(specName string, key ast.SortKey) error {
	for i, col := range key.Columns {
		if i == 0 {
			continue
		}
		if key.IsNullable(col) {
			return fmt.Errorf("sort spec %q key %q: only the leading column may be nullable; %q is not", specName, key.Name, col)
		}
	}
	return nil
}

func validateCursorBindings(spec *ast.SortSpec) error {
	need := func(col string) error {
		if _, ok := spec.Cursor[col]; !ok {
			return fmt.Errorf("sort spec %q: no cursor binding for column %q", spec.Name, col)
		}
		return nil
	}
	for _, k := range spec.Keys {
		for _, c := range k.Columns {
			if err := need(c); err != nil {
				return err
			}
		}
	}
	for _, c := range spec.DefaultBy {
		if err := need(c); err != nil {
			return err
		}
	}
	return nil
}

func splitList(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// splitTerms splits on top-level commas only, so expression terms like
// COALESCE(a, b, c) survive as a single term.
func splitTerms(s string) []string {
	var out []string
	depth := 0
	start := 0
	flush := func(end int) {
		if p := strings.TrimSpace(s[start:end]); p != "" {
			out = append(out, p)
		}
	}
	for i, r := range s {
		switch r {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				flush(i)
				start = i + 1
			}
		}
	}
	flush(len(s))
	return out
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// expandPaginatedQueries replaces every query carrying a paginate=<spec> pragma
// with one fanned-out SourceQuery per sort key and direction, plus a default.
// Each variant is a fully concrete query (clean ORDER BY + matching cursor
// predicate) re-parsed through the normal path so it gets standard $n
// numbering and type inference.
func (p *parser) expandPaginatedQueries(file *ast.File) {
	specs, err := collectSortSpecs(file.Comments)
	if err != nil {
		p.error(gotok.Pos(1), err.Error())
		return
	}

	var out []ast.Query
	for _, q := range file.Queries {
		sq, ok := q.(*ast.SourceQuery)
		if !ok || sq.Pragmas.Paginate == "" {
			out = append(out, q)
			continue
		}
		spec, ok := specs[sq.Pragmas.Paginate]
		if !ok {
			p.error(sq.Pos(), fmt.Sprintf("query %s references unknown sort spec %q", sq.Name, sq.Pragmas.Paginate))
			return
		}
		if sq.Pragmas.OutputType == "" {
			p.error(sq.Pos(), fmt.Sprintf("query %s uses paginate=%s but has no output= pragma; a paginated query must declare a shared output row type so every variant shares one return type", sq.Name, sq.Pragmas.Paginate))
			return
		}
		variants, err := p.fanOut(sq, spec)
		if err != nil {
			p.error(sq.Pos(), err.Error())
			return
		}
		out = append(out, variants...)
	}
	file.Queries = out
}

func (p *parser) fanOut(sq *ast.SourceQuery, spec *ast.SortSpec) ([]ast.Query, error) {
	type variant struct {
		key      ast.VariantKey
		nameSfx  string
		orderBy  string
		keyset   string
	}
	var variants []variant

	keyset := spec.IsKeyset()
	withTiebreak := func(orderBy string) string {
		if spec.Tiebreak == "" {
			return orderBy
		}
		return orderBy + ", " + spec.Tiebreak
	}
	keysetFor := func(columns, nullable []string, desc bool) string {
		if !keyset {
			return ""
		}
		return keysetPredicate(spec, columns, nullable, desc)
	}

	variants = append(variants, variant{
		key:     ast.VariantKey{IsDefault: true},
		nameSfx: "default",
		orderBy: withTiebreak(orderByClause(spec.DefaultBy, nil, false, keyset)),
		keyset:  keysetFor(spec.DefaultBy, nil, false),
	})
	for _, k := range spec.Keys {
		for _, desc := range []bool{true, false} {
			dir := "asc"
			if desc {
				dir = "desc"
			}
			variants = append(variants, variant{
				key:     ast.VariantKey{SortKey: k.Name, Descending: desc},
				nameSfx: k.Name + "_" + dir,
				orderBy: withTiebreak(orderByClause(k.Columns, k.Nullable, desc, keyset)),
				keyset:  keysetFor(k.Columns, k.Nullable, desc),
			})
		}
	}

	keysetRe := placeholderRegexp("keyset", spec.Name)
	orderByRe := placeholderRegexp("orderby", spec.Name)
	hasKeysetPlaceholder := keysetRe.MatchString(sq.SourceSQL)
	if keyset && !hasKeysetPlaceholder {
		return nil, fmt.Errorf("query %s: keyset spec %q set but no pggen.keyset('%s') in query", sq.Name, spec.Name, spec.Name)
	}
	if !keyset && hasKeysetPlaceholder {
		return nil, fmt.Errorf("query %s: spec %q has no cursor bindings but query uses pggen.keyset('%s')", sq.Name, spec.Name, spec.Name)
	}
	if !orderByRe.MatchString(sq.SourceSQL) {
		return nil, fmt.Errorf("query %s: paginate spec %q set but no pggen.orderby('%s') in query", sq.Name, spec.Name, spec.Name)
	}

	annotation := variantAnnotation(sq)
	out := make([]ast.Query, 0, len(variants))
	for _, v := range variants {
		body := sq.SourceSQL
		if keyset {
			body = keysetRe.ReplaceAllString(body, v.keyset)
		}
		body = orderByRe.ReplaceAllString(body, v.orderBy)
		varName := sq.Name + "__" + v.nameSfx
		// SourceSQL includes the trailing ';'; normalize so we emit exactly one.
		body = strings.TrimRight(body, " \t\n;")
		src := "-- name: " + varName + " " + annotation + "\n" + body + ";\n"

		// Use a fresh FileSet per variant: ParseFile assumes single-file
		// (base-1) position offsets when slicing the source.
		sub, err := ParseFile(gotok.NewFileSet(), "<"+varName+">", src, p.mode)
		if err != nil {
			return nil, fmt.Errorf("expand variant %s: %w", varName, err)
		}
		if len(sub.Queries) != 1 {
			return nil, fmt.Errorf("expand variant %s: expected 1 query, got %d", varName, len(sub.Queries))
		}
		vq, ok := sub.Queries[0].(*ast.SourceQuery)
		if !ok {
			return nil, fmt.Errorf("expand variant %s: bad query", varName)
		}
		vq.VariantGroup = sq.Name
		vq.VariantKey = v.key
		out = append(out, vq)
	}
	return out, nil
}

// variantAnnotation reproduces the original query's result kind and output
// pragmas (dropping paginate=) for the synthesized variant source.
func variantAnnotation(sq *ast.SourceQuery) string {
	sb := strings.Builder{}
	sb.WriteString(string(sq.ResultKind))
	if sq.Pragmas.OutputType != "" {
		sb.WriteString(" output=" + sq.Pragmas.OutputType)
	}
	if sq.Pragmas.ProtobufType != "" {
		sb.WriteString(" proto-type=" + sq.Pragmas.ProtobufType)
	}
	return sb.String()
}

func placeholderRegexp(fn, spec string) *regexp.Regexp {
	return regexp.MustCompile(`pggen\.` + fn + `\(\s*'` + regexp.QuoteMeta(spec) + `'\s*\)`)
}

// orderByClause builds a clean, index-friendly ORDER BY column list. In keyset
// mode the NULLS position is coupled to the direction (DESC NULLS FIRST / ASC
// NULLS LAST) so it matches the cursor predicate; in ordering-only mode nullable
// columns are NULLS LAST in both directions.
func orderByClause(columns, nullable []string, desc, keysetMode bool) string {
	dir := "ASC"
	nulls := " NULLS LAST"
	if desc {
		dir = "DESC"
		if keysetMode {
			nulls = " NULLS FIRST"
		}
	}
	parts := make([]string, len(columns))
	for i, c := range columns {
		s := c + " " + dir
		if contains(nullable, c) {
			s += nulls
		}
		parts[i] = s
	}
	return strings.Join(parts, ", ")
}

// keysetPredicate builds the cursor "strictly after" predicate for the given
// columns and direction, with a first-page escape when all cursor args are
// NULL. Only the leading column may be nullable (validated earlier).
func keysetPredicate(spec *ast.SortSpec, columns, nullable []string, desc bool) string {
	op := ">"
	if desc {
		op = "<"
	}
	arg := func(col string) string { return "pggen.arg('" + spec.Cursor[col] + "')" }

	rowComparison := func(cols []string) string {
		if len(cols) == 1 {
			return cols[0] + " " + op + " " + arg(cols[0])
		}
		lhs := strings.Join(cols, ", ")
		rhs := make([]string, len(cols))
		for i, c := range cols {
			rhs[i] = arg(c)
		}
		return "(" + lhs + ") " + op + " (" + strings.Join(rhs, ", ") + ")"
	}

	var main string
	leadNullable := len(columns) > 0 && contains(nullable, columns[0])
	if !leadNullable {
		main = rowComparison(columns)
	} else {
		c1 := columns[0]
		rest := columns[1:]
		nullArm := c1 + " IS NULL AND " + arg(c1) + " IS NOT NULL"
		if desc {
			nullArm = c1 + " IS NOT NULL AND " + arg(c1) + " IS NULL"
		}
		var sameLead string
		if len(rest) > 0 {
			sameLead = "(" + c1 + " IS NOT DISTINCT FROM " + arg(c1) + " AND " + rowComparison(rest) + ")"
		}
		earlier := "(" + c1 + " " + op + " " + arg(c1) + ")"
		arms := []string{}
		if sameLead != "" {
			arms = append(arms, sameLead)
		}
		arms = append(arms, earlier, "("+nullArm+")")
		main = strings.Join(arms, "\n        OR ")
	}

	firstPage := make([]string, len(columns))
	for i, c := range columns {
		firstPage[i] = arg(c) + " IS NULL"
	}
	escape := "(" + strings.Join(firstPage, " AND ") + ")"

	return "(" + main + "\n        OR " + escape + ")"
}
