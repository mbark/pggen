package ch

import "strings"

// InsertTarget is where an INSERT writes: the table it names, and the columns
// it lists, if it lists any.
type InsertTarget struct {
	// Table as the query spells it, qualified or not, with any quoting
	// removed: `source`.`raw` and source.raw both give "source.raw". This is
	// the name to show in an error; to name the table in another statement,
	// use SQLName.
	Table string
	// SQLName is the table exactly as the query spells it, quoting and all.
	//
	// Unquoting loses which dots separated identifiers and which were inside
	// one: `source.raw` is a single table whose name holds a dot, and Table
	// gives it as source.raw, the same as the qualified `source`.`raw`. So a
	// statement that names the table back to ClickHouse spells it the way the
	// query did rather than the way Table reads.
	SQLName string
	// Columns names in the order the query lists them. Empty for an INSERT
	// that gives no column list, which is legal and means every column.
	Columns []string
}

// ScanInsertTarget reads the target off an INSERT statement.
//
// It exists because an INSERT is the one statement ClickHouse will not analyse
// without running: DESCRIBE and EXPLAIN QUERY TREE are both SELECT-shaped, so
// an :exec query gets no more than a parse. The target is the half that can
// still be checked cheaply — DESCRIBE TABLE resolves it and reads no data —
// and it is where a schema change lands.
//
// ok is false for anything this cannot read a plain table out of: a statement
// that is not an INSERT, or an INSERT INTO FUNCTION, which writes through a
// table function that has no schema to describe. A false ok means "nothing to
// check here", never "this query is wrong".
func ScanInsertTarget(sql string) (InsertTarget, bool) {
	i := skipSpaceAndComments(sql, 0)

	word, next := readWord(sql, i)
	if !strings.EqualFold(word, "INSERT") {
		return InsertTarget{}, false
	}
	i = skipSpaceAndComments(sql, next)

	word, next = readWord(sql, i)
	if !strings.EqualFold(word, "INTO") {
		return InsertTarget{}, false
	}
	i = skipSpaceAndComments(sql, next)

	// INSERT INTO TABLE t is a legal spelling of INSERT INTO t. INSERT INTO
	// FUNCTION writes through a table function, which has no schema to
	// describe.
	word, next = readWord(sql, i)
	switch {
	case strings.EqualFold(word, "FUNCTION"):
		return InsertTarget{}, false
	case strings.EqualFold(word, "TABLE"):
		i = skipSpaceAndComments(sql, next)
	}

	table, afterTable := readQualifiedIdent(sql, i)
	if table == "" {
		return InsertTarget{}, false
	}
	target := InsertTarget{Table: table, SQLName: sql[i:afterTable]}
	i = skipSpaceAndComments(sql, afterTable)

	// No column list is legal and means every column, in order. There is
	// nothing to check against the table, but the table itself still is.
	if i >= len(sql) || sql[i] != '(' {
		return target, true
	}

	columns, ok := readColumnList(sql, i)
	if !ok {
		return InsertTarget{}, false
	}
	target.Columns = columns
	return target, true
}

// readColumnList reads the parenthesised column list starting at the '(' at i.
// It gives up rather than guess if it meets anything but names and commas,
// since an expression there is not a column this can check.
func readColumnList(sql string, i int) ([]string, bool) {
	var columns []string
	i = skipSpaceAndComments(sql, i+1)
	for i < len(sql) {
		if sql[i] == ')' {
			return columns, true
		}
		name, next := readIdent(sql, i)
		if name == "" {
			return nil, false
		}
		columns = append(columns, name)
		i = skipSpaceAndComments(sql, next)
		if i < len(sql) && sql[i] == ',' {
			i = skipSpaceAndComments(sql, i+1)
		}
	}
	return nil, false
}

// readQualifiedIdent reads an identifier that may carry a database qualifier,
// returning it with the dot kept and the quoting dropped.
func readQualifiedIdent(sql string, i int) (string, int) {
	name, next := readIdent(sql, i)
	if name == "" {
		return "", i
	}
	// No skipSpaceAndComments around the dot: ClickHouse does not accept
	// `db . table`, and treating it as qualified would swallow the next token.
	if next < len(sql) && sql[next] == '.' {
		qualified, after := readIdent(sql, next+1)
		if qualified == "" {
			return "", i
		}
		return name + "." + qualified, after
	}
	return name, next
}

// readIdent reads one identifier, bare or quoted, and returns it unquoted.
func readIdent(sql string, i int) (string, int) {
	if i >= len(sql) {
		return "", i
	}
	if next := skipLiteral(sql, i); next != i {
		// A backtick or double quote makes an identifier; a single quote
		// makes a string, which is not one.
		if sql[i] == '\'' {
			return "", i
		}
		return unescapeQuoted(sql[i:next]), next
	}
	word, next := readWord(sql, i)
	return word, next
}

// unescapeQuoted strips the quotes from a quoted identifier and undoes the
// backslash escapes inside it. An identifier whose quote is never closed runs
// to the end of the statement and is not one, so it gives "".
func unescapeQuoted(quoted string) string {
	if len(quoted) < 2 || quoted[len(quoted)-1] != quoted[0] {
		return ""
	}
	inner := quoted[1 : len(quoted)-1]
	if !strings.Contains(inner, "\\") {
		return inner
	}
	sb := &strings.Builder{}
	for i := 0; i < len(inner); i++ {
		if inner[i] == '\\' && i+1 < len(inner) {
			i++
		}
		sb.WriteByte(inner[i])
	}
	return sb.String()
}

// readWord reads the run of identifier characters at i.
func readWord(sql string, i int) (string, int) {
	j := i
	for j < len(sql) && isIdentByte(sql[j]) {
		j++
	}
	return sql[i:j], j
}

// skipSpaceAndComments returns the index of the next byte at i or after that
// is neither whitespace nor part of a comment.
func skipSpaceAndComments(sql string, i int) int {
	for i < len(sql) {
		if c := sql[i]; c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			i++
			continue
		}
		if next := skipComment(sql, i); next != i {
			i = next
			continue
		}
		return i
	}
	return i
}
