package chdocker

type chTemplate struct {
	InitScripts []string
}

// The official image runs any .sql or .sh file in the init directory once, on
// first start, against CLICKHOUSE_DB.
const dockerfileTemplate = `
{{- /*gotype: github.com/mbark/pggen/internal/chdocker.chTemplate*/ -}}
{{- define "dockerfile" -}}
FROM clickhouse/clickhouse-server:25.3
{{ range .InitScripts }}
COPY {{.}} /docker-entrypoint-initdb.d/
{{ end }}
{{- end }}
`
