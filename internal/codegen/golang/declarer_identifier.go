package golang

// ChIdentifierDeclarer emits substituteIdentifiers, the helper a generated
// ClickHouse query calls to fill its {…:Identifier} holes.
//
// One per package, however many queries take an identifier.
type ChIdentifierDeclarer struct{}

func NewChIdentifierDeclarer() ChIdentifierDeclarer { return ChIdentifierDeclarer{} }

func (d ChIdentifierDeclarer) DedupeKey() string { return "func::02_substitute_identifiers" }

func (d ChIdentifierDeclarer) Declare(string) (string, error) {
	return chIdentifierHelper, nil
}

// chIdentifierHelper is the only place generated code puts a caller's string
// into SQL text rather than binding it, so it is also the only place that has
// to say why that is safe.
//
// ClickHouse would bind an Identifier itself, and quote it — but clickhouse-go
// switches a query to server-side parameters as soon as its text holds any
// {…:…}, and server-side parameters travel as text the driver renders wrongly
// for time.Time, uuid.UUID and decimal.Decimal. One Identifier left for the
// server would break every other parameter in the same query.
//
// What makes the substitution safe is the check, not the escaping: an
// identifier that needs quoting is refused rather than quoted. Every real
// table and column name is a plain identifier, and refusing the rest means
// there is no escaping to get wrong.
const chIdentifierHelper = `// substituteIdentifiers fills each {name:Identifier} in sql with the value
// given for name.
//
// A ClickHouse Identifier parameter names a table or column, so it cannot be
// bound like a value: it has to be in the query text before the driver sees
// it. Each value must therefore be a plain identifier — a letter or underscore
// followed by letters, digits or underscores, optionally qualified by a
// database — and anything else is refused rather than quoted, so no value can
// carry SQL of its own into the query.
func substituteIdentifiers(sql string, values map[string]string) (string, error) {
	for name, value := range values {
		if err := checkIdentifier(name, value); err != nil {
			return "", err
		}
		sql = strings.ReplaceAll(sql, "{"+name+":Identifier}", value)
	}
	return sql, nil
}

// checkIdentifier reports whether value is a plain, optionally qualified
// ClickHouse identifier. param names the query parameter, for the error.
func checkIdentifier(param, value string) error {
	if value == "" {
		return fmt.Errorf("identifier parameter %s is empty", param)
	}
	for _, part := range strings.Split(value, ".") {
		if part == "" {
			return fmt.Errorf("identifier parameter %s has an empty part in %q", param, value)
		}
		for i, r := range part {
			isLetter := r == '_' || ('a' <= r && r <= 'z') || ('A' <= r && r <= 'Z')
			if isLetter || (i > 0 && '0' <= r && r <= '9') {
				continue
			}
			return fmt.Errorf("identifier parameter %s is %q, which is not a plain identifier: "+
				"a table or column name here must be letters, digits and underscores, "+
				"optionally qualified by a database", param, value)
		}
	}
	return nil
}`
