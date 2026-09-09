package chinfer

import (
	"fmt"
	"strings"

	"github.com/mbark/pggen/internal/ch"
)

// Param is a {name:Type} query parameter.
type Param struct {
	Name string
	Type ch.Type
}

// ScanParams finds the ClickHouse query parameters in sql, in order of first
// appearance.
//
// This is where ClickHouse differs most from Postgres. Postgres infers a
// parameter's type server-side from where it appears, so pggen never has to be
// told. ClickHouse spells a parameter {name:Type} with the type written by
// hand, which means the type is already in the query text and no round trip is
// needed to learn it.
//
// A parameter may appear more than once; repeats must agree on the type and
// collapse to a single input. String literals and comments are skipped so a
// brace inside them is not mistaken for a parameter.
func ScanParams(sql string) ([]Param, error) {
	var params []Param
	seen := make(map[string]ch.Type)

	for i := 0; i < len(sql); i++ {
		switch c := sql[i]; c {
		case '\'', '`', '"':
			// A string literal or quoted identifier.
			quote := c
			for i++; i < len(sql); i++ {
				if sql[i] == '\\' {
					i++
					continue
				}
				if sql[i] == quote {
					break
				}
			}
		case '-':
			if i+1 < len(sql) && sql[i+1] == '-' {
				for i < len(sql) && sql[i] != '\n' {
					i++
				}
			}
		case '/':
			if i+1 < len(sql) && sql[i+1] == '*' {
				i += 2
				for i+1 < len(sql) && (sql[i] != '*' || sql[i+1] != '/') {
					i++
				}
				i++
			}
		case '{':
			end := strings.IndexByte(sql[i:], '}')
			if end < 0 {
				return nil, fmt.Errorf("unclosed { in query; a ClickHouse parameter looks like {name:Type}")
			}
			body := sql[i+1 : i+end]
			i += end

			colon := strings.IndexByte(body, ':')
			if colon < 0 {
				return nil, fmt.Errorf("query parameter {%s} is missing its type; "+
					"ClickHouse parameters look like {%s:String}", body, strings.TrimSpace(body))
			}
			name := strings.TrimSpace(body[:colon])
			if name == "" {
				return nil, fmt.Errorf("query parameter {%s} is missing its name", body)
			}
			typ, err := ch.Parse(strings.TrimSpace(body[colon+1:]))
			if err != nil {
				return nil, fmt.Errorf("query parameter %q: %w", name, err)
			}
			if prev, ok := seen[name]; ok {
				if prev.String() != typ.String() {
					return nil, fmt.Errorf("query parameter %q is declared as both %s and %s; "+
						"every occurrence must use the same type", name, prev, typ)
				}
				continue
			}
			seen[name] = typ
			params = append(params, Param{Name: name, Type: typ})
		}
	}
	return params, nil
}
