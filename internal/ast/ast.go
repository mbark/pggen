package ast

import gotok "go/token"

// Node is the super-type of all AST nodes.
type Node interface {
	Pos() gotok.Pos
	End() gotok.Pos
	Kind() NodeKind
}

// NodeKind is the kind of Node.
type NodeKind int

const (
	KindLineComment NodeKind = iota
	KindCommentGroup
	KindBadQuery
	KindTemplateQuery
	KindFile
)

var kindNames = [...]string{
	KindLineComment:   "LineComment",
	KindCommentGroup:  "CommentGroup",
	KindBadQuery:      "BadQuery",
	KindTemplateQuery: "SourceQuery",
	KindFile:          "File",
}

func (k NodeKind) String() string {
	return kindNames[k]
}

// Query nodes implement the Decl interface.
type Query interface {
	Node
	queryNode()
}

// ----------------------------------------------------------------------------
// Comments

// A LineComment node represents a single line comment.
type LineComment struct {
	Start gotok.Pos // position of the '--' starting the comment
	Text  string    // comment text excluding '\n'
}

func (c *LineComment) Pos() gotok.Pos { return c.Start }
func (c *LineComment) End() gotok.Pos { return gotok.Pos(int(c.Start) + len(c.Text)) }
func (c *LineComment) Kind() NodeKind { return KindLineComment }

// A CommentGroup represents a sequence of comments with no other tokens and
// no empty lines between.
type CommentGroup struct {
	List []*LineComment // len(List) > 0
}

func (g *CommentGroup) Pos() gotok.Pos { return g.List[0].Pos() }
func (g *CommentGroup) End() gotok.Pos { return g.List[len(g.List)-1].End() }
func (g *CommentGroup) Kind() NodeKind { return KindCommentGroup }

// ----------------------------------------------------------------------------
// Queries

// ResultKind is the shape of the output. Controls the output type of the query.
type ResultKind string

const (
	ResultKindMany ResultKind = ":many"
	ResultKindOne  ResultKind = ":one"
	ResultKindExec ResultKind = ":exec"
)

// Pragmas are options to control generated code for a single query.
type Pragmas struct {
	ProtobufType string // package qualified protocol buffer message type to use for output rows
	OutputType   string // user-specified output row struct name, e.g. "ItemRow"
	Paginate     string // name of the -- sort: spec to fan this query out over, e.g. "payments_sort"
}

// SortSpec is a keyset-pagination spec declared in a "-- sort: <name>" comment
// block. A query with the paginate=<name> pragma is fanned out into one
// concrete statement per sort key and direction, plus a default, each with a
// clean ORDER BY and a matching cursor predicate.
type SortSpec struct {
	Name      string          // spec name, e.g. "payments_sort"
	Keys      []SortKey       // declared sort keys
	DefaultBy []string        // columns for the default ordering (no sort key given)
	Cursor    map[string]string // column -> cursor pggen.arg name; empty means ordering-only (no keyset predicate)
	Tiebreak  string          // raw ORDER BY terms appended to every variant, e.g. "created_at DESC"
}

// IsKeyset reports whether this spec drives a keyset cursor predicate. Specs
// without cursor bindings are ordering-only (offset pagination): they fan out
// the ORDER BY but emit no pggen.keyset predicate.
func (s SortSpec) IsKeyset() bool { return len(s.Cursor) > 0 }

// SortKey is one runtime-selectable sort dimension within a SortSpec.
type SortKey struct {
	Name     string   // runtime SortOrder.Key value, e.g. "payment_date"
	Columns  []string // ordered sort columns, the last being the unique tiebreaker
	Nullable []string // subset of Columns that are nullable
}

// IsNullable reports whether col is declared nullable for this key.
func (k SortKey) IsNullable(col string) bool {
	for _, c := range k.Nullable {
		if c == col {
			return true
		}
	}
	return false
}

// An query is represented by one of the following query nodes.
type (
	// A BadQuery node is a placeholder for queries containing syntax errors
	// for which no correct declaration nodes can be created.
	BadQuery struct {
		From, To gotok.Pos // position range of bad declaration
	}

	// An SourceQuery node represents a query entry from the source code.
	SourceQuery struct {
		Name        string        // name of the query
		Doc         *CommentGroup // associated documentation; or nil
		Start       gotok.Pos     // position of the start token, like 'SELECT' or 'UPDATE'
		SourceSQL   string        // the complete sql query as it appeared in the source file
		PreparedSQL string        // the sql query with args replaced by $1, $2, etc.
		ParamNames  []string      // the name of each param in the PreparedSQL, the nth entry is the $n+1 param
		ResultKind  ResultKind    // the result output type
		Pragmas     Pragmas       // optional query options
		Semi        gotok.Pos     // position of the closing semicolon

		// Set when this query was produced by fanning out a paginate=<spec>
		// query. VariantGroup is the public dispatcher name (the original query
		// name); VariantKey identifies the sort key + direction. Empty for
		// ordinary queries.
		VariantGroup string
		VariantKey   VariantKey
	}
)

// VariantKey identifies one fanned-out statement within a paginated query
// group.
type VariantKey struct {
	SortKey    string // the SortKey.Name, empty for the default variant
	Descending bool   // direction; ignored for the default variant
	IsDefault  bool   // true for the no-sort-key default variant
}

func (q *BadQuery) Pos() gotok.Pos { return q.From }
func (q *BadQuery) End() gotok.Pos { return q.To }
func (q *BadQuery) Kind() NodeKind { return KindBadQuery }
func (*BadQuery) queryNode()       {}

func (q *SourceQuery) Pos() gotok.Pos { return q.Start }
func (q *SourceQuery) End() gotok.Pos { return q.Semi }
func (q *SourceQuery) Kind() NodeKind { return KindTemplateQuery }
func (*SourceQuery) queryNode()       {}

// ----------------------------------------------------------------------------
// Files and packages

// A File node represents a query source file.
//
// The Comments list contains all comments in the source file in order of
// appearance, including the comments that are pointed to from other nodes
// via Doc and Comment fields.
type File struct {
	Name     string
	Doc      *CommentGroup   // associated documentation; or nil
	Queries  []Query         // top-level queries; or nil
	Comments []*CommentGroup // list of all comments in the source file
}

func (f *File) Pos() gotok.Pos { return gotok.Pos(1) }
func (f *File) End() gotok.Pos {
	if n := len(f.Queries); n > 0 {
		return f.Queries[n-1].End()
	}
	return gotok.Pos(1)
}
func (f *File) Kind() NodeKind { return KindFile }
