package triggers

import "regexp"

// templateVarPattern matches `{{ key }}` placeholders (with optional surrounding
// whitespace). Keys are dotted field names exposed by a source adapter, e.g.
// `{{ pr.title }}`.
var templateVarPattern = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.]+)\s*\}\}`)

// renderTemplate substitutes `{{ key }}` placeholders in a trigger's prompt
// template with values from the adapter's field map. This is the minimal v1
// mechanism (resolves the PRD's open question): restricted substitution over an
// allow-listed field set — no expressions, no arbitrary payload access. Unknown
// keys render as empty strings.
//
// Payload-injection hardening (treating field values as untrusted) is explicitly
// deferred per the PRD; values are substituted verbatim.
func renderTemplate(tmpl string, fields map[string]string) string {
	return templateVarPattern.ReplaceAllStringFunc(tmpl, func(match string) string {
		key := templateVarPattern.FindStringSubmatch(match)[1]
		return fields[key]
	})
}
