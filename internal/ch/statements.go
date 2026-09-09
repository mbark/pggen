package ch

import (
	"fmt"
	"strings"
)

// skipLiteral returns the index just past the string literal or quoted
// identifier starting at i, or i if none starts there.
//
// An unterminated literal runs to the end of the input rather than erroring:
// the callers are looking for something else, and ClickHouse gives a better
// message for a malformed query than pggen can.
func skipLiteral(sql string, i int) int {
	quote := sql[i]
	if quote != '\'' && quote != '`' && quote != '"' {
		return i
	}
	for j := i + 1; j < len(sql); j++ {
		if sql[j] == '\\' {
			j++
			continue
		}
		if sql[j] == quote {
			return j + 1
		}
	}
	return len(sql)
}

// skipComment returns the index just past the comment starting at i, or i if
// none starts there. A line comment stops at its newline, leaving the newline
// to the caller.
func skipComment(sql string, i int) int {
	switch {
	case sql[i] == '-' && i+1 < len(sql) && sql[i+1] == '-':
		for j := i + 2; j < len(sql); j++ {
			if sql[j] == '\n' {
				return j
			}
		}
		return len(sql)
	case sql[i] == '/' && i+1 < len(sql) && sql[i+1] == '*':
		for j := i + 2; j+1 < len(sql); j++ {
			if sql[j] == '*' && sql[j+1] == '/' {
				return j + 2
			}
		}
		return len(sql)
	}
	return i
}

// SplitStatements splits sql on the semicolons that separate statements,
// ignoring any inside a string literal, a quoted identifier, or a comment.
//
// ClickHouse runs one statement per call, so a schema file has to be taken
// apart before it can be loaded. Comments are dropped rather than carried
// along, so that a file ending in one does not produce a statement with no
// query in it.
func SplitStatements(sql string) []string {
	var stmts []string
	sb := &strings.Builder{}
	flush := func() {
		if stmt := strings.TrimSpace(sb.String()); stmt != "" {
			stmts = append(stmts, stmt)
		}
		sb.Reset()
	}
	for i := 0; i < len(sql); {
		if next := skipComment(sql, i); next != i {
			// A comment separates the tokens around it, so it can't just
			// vanish: SELECT 1--c\nFROM t would become SELECT 1FROM t.
			sb.WriteByte('\n')
			i = next
			continue
		}
		if next := skipLiteral(sql, i); next != i {
			sb.WriteString(sql[i:next])
			i = next
			continue
		}
		if sql[i] == ';' {
			flush()
			i++
			continue
		}
		sb.WriteByte(sql[i])
		i++
	}
	flush()
	return stmts
}

// SingleStatement returns sql as one statement, with comments and the
// statement terminator removed.
//
// It exists because inference does not send the query as written: it splices
// it into DESCRIBE (...) or after EXPLAIN AST, where a trailing semicolon is a
// syntax error and a trailing -- comment swallows whatever follows it. Rather
// than have each wrapper defend itself, they all get a statement that has
// neither.
func SingleStatement(sql string) (string, error) {
	stmts := SplitStatements(sql)
	switch len(stmts) {
	case 1:
		return stmts[0], nil
	case 0:
		return "", fmt.Errorf("the query is empty")
	default:
		return "", fmt.Errorf(
			"the query has %d statements separated by semicolons; "+
				"pggen generates one method per query, so write them as separate queries",
			len(stmts))
	}
}
