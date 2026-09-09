// Package ident turns catalog names into exported Go identifiers.
package ident

import "strings"

// Export turns "tool.execute.before", "cursor-agent", or "PreToolUse" into
// an exported identifier: it splits on '_', '-', and '.', then upper-cases
// the first letter of each part and joins them.
func Export(name string) string {
	parts := strings.FieldsFunc(name, func(r rune) bool { return r == '_' || r == '-' || r == '.' })
	for i, p := range parts {
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}
