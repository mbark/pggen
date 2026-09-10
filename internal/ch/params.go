package ch

import (
	"fmt"
	"strings"
)

// Param is a {name:Type} query parameter.
type Param struct {
	Name string
	Type Type
}

// scanSQL walks sql and calls onParam for each {name:Type} it finds, and
// onText for every stretch of SQL between them.
//
// It skips string literals, quoted identifiers, and comments, so a brace
// inside one is not mistaken for a parameter.
func scanSQL(sql string, onText func(string), onParam func(Param) error) error {
	prev := 0
	emitText := func(upto int) {
		if onText != nil && upto > prev {
			onText(sql[prev:upto])
		}
	}
	for i := 0; i < len(sql); {
		// SplitStatements walks the same way.
		if next := skipComment(sql, i); next != i {
			i = next
			continue
		}
		if next := skipLiteral(sql, i); next != i {
			i = next
			continue
		}
		if sql[i] == '{' {
			end := strings.IndexByte(sql[i:], '}')
			if end < 0 {
				return fmt.Errorf("unclosed { in query; a ClickHouse parameter looks like {name:Type}")
			}
			body := sql[i+1 : i+end]

			colon := strings.IndexByte(body, ':')
			if colon < 0 {
				return fmt.Errorf("query parameter {%s} is missing its type; "+
					"ClickHouse parameters look like {%s:String}", body, strings.TrimSpace(body))
			}
			name := strings.TrimSpace(body[:colon])
			if name == "" {
				return fmt.Errorf("query parameter {%s} is missing its name", body)
			}
			typ, err := Parse(strings.TrimSpace(body[colon+1:]))
			if err != nil {
				return fmt.Errorf("query parameter %q: %w", name, err)
			}

			emitText(i)
			if err := onParam(Param{Name: name, Type: typ}); err != nil {
				return err
			}
			i += end + 1
			prev = i
			continue
		}
		i++
	}
	emitText(len(sql))
	return nil
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
// collapse to a single input.
func ScanParams(sql string) ([]Param, error) {
	var params []Param
	seen := make(map[string]Type)
	err := scanSQL(sql, nil, func(p Param) error {
		if prev, ok := seen[p.Name]; ok {
			if prev.String() != p.Type.String() {
				return fmt.Errorf("query parameter %q is declared as both %s and %s; "+
					"every occurrence must use the same type", p.Name, prev, p.Type)
			}
			return nil
		}
		seen[p.Name] = p.Type
		params = append(params, p)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return params, nil
}

// IsIdentifier reports whether t is ClickHouse's Identifier parameter type,
// which names a table or column rather than carrying a value.
//
// It is the one parameter type pggen substitutes into the query text instead of
// binding: see SubstituteIdentifiers.
func IsIdentifier(t Type) bool {
	scalar, ok := t.(Scalar)
	return ok && scalar.Name == "Identifier"
}

// SubstituteIdentifiers replaces each {name:Identifier} in sql with value(name),
// leaving every other parameter alone. A name value returns "" for is left as
// it was.
//
// ClickHouse can bind an Identifier parameter itself, and does it safely — it
// quotes the value as a single identifier, so an injection attempt becomes an
// unknown table rather than SQL. pggen cannot use that: clickhouse-go switches
// a query to server-side parameters the moment its text contains any {…:…}, and
// server-side parameters travel as text that the driver renders wrongly for
// time.Time, uuid.UUID and decimal.Decimal — which is the whole reason
// RewriteParams exists. One Identifier parameter would therefore break every
// other parameter in the same query.
//
// So the identifier is substituted into the text before the driver sees it, and
// the generated code checks the value is a plain identifier first.
func SubstituteIdentifiers(sql string, value func(name string) string) (string, error) {
	sb := &strings.Builder{}
	sb.Grow(len(sql))
	err := scanSQL(sql,
		func(text string) { sb.WriteString(text) },
		func(p Param) error {
			if v := value(p.Name); IsIdentifier(p.Type) && v != "" {
				sb.WriteString(v)
				return nil
			}
			sb.WriteString("{")
			sb.WriteString(p.Name)
			sb.WriteString(":")
			sb.WriteString(p.Type.String())
			sb.WriteString("}")
			return nil
		})
	if err != nil {
		return "", err
	}
	return sb.String(), nil
}

// RewriteParams turns each {name:Type} into cast(@name AS Type).
//
// It exists because ClickHouse's server-side parameters travel as text, and
// clickhouse-go does not render every Go value into text the server will
// accept: a time.Time arrives as a Unix timestamp, and a uuid.UUID or a
// decimal.Decimal arrives quoted, all of which the server rejects. The @name
// form uses the driver's client-side binding instead, which renders every type
// correctly, and the cast keeps the declared type explicit.
//
// The rewrite happens at generation time, so the query file itself stays a
// query you can paste into clickhouse-client.
func RewriteParams(sql string) (string, error) {
	sb := &strings.Builder{}
	sb.Grow(len(sql))
	err := scanSQL(sql,
		func(text string) { sb.WriteString(text) },
		func(p Param) error {
			// An Identifier names a table or column, so there is nothing to
			// cast and nothing to bind. It stays in the text as a hole the
			// generated code fills; see SubstituteIdentifiers.
			if IsIdentifier(p.Type) {
				sb.WriteString("{")
				sb.WriteString(p.Name)
				sb.WriteString(":Identifier}")
				return nil
			}
			sb.WriteString("cast(@")
			sb.WriteString(p.Name)
			sb.WriteString(" AS ")
			sb.WriteString(p.Type.String())
			sb.WriteString(")")
			return nil
		})
	if err != nil {
		return "", err
	}
	return sb.String(), nil
}

// RenameParams rewrites each {name:Type} to {rename(name):Type}.
//
// It exists because ClickHouse carries server-side query parameters in the
// same map as query settings, and `limit` and `offset` are both real settings.
// A parameter named after one of them makes the server reject the query with
// "Cannot parse quoted string" — and it does so even for a query that never
// mentions the parameter, since the collision happens while the settings are
// read, before the SQL is parsed.
//
// Only inference sends parameters that way, so only inference needs this: it
// describes a copy of the query whose parameters are renamed out of the
// settings namespace. Generated code is unaffected, because it binds
// client-side through cast(@name AS Type); see RewriteParams.
func RenameParams(sql string, rename func(string) string) (string, error) {
	sb := &strings.Builder{}
	sb.Grow(len(sql))
	err := scanSQL(sql,
		func(text string) { sb.WriteString(text) },
		func(p Param) error {
			sb.WriteString("{")
			sb.WriteString(rename(p.Name))
			sb.WriteString(":")
			sb.WriteString(p.Type.String())
			sb.WriteString("}")
			return nil
		})
	if err != nil {
		return "", err
	}
	return sb.String(), nil
}
