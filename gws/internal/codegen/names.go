package codegen

import (
	"strings"
	"unicode"
)

func typeName(name, prefix string) string {
	parts := splitIdentifierWords(prefix + " " + name)
	if len(parts) == 0 {
		return ""
	}

	var b strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}

		rs := []rune(strings.ToLower(part))
		rs[0] = unicode.ToUpper(rs[0])
		b.WriteString(string(rs))
	}

	return b.String()
}

func splitIdentifierWords(value string) []string {
	var parts []string
	var current []rune

	flush := func() {
		if len(current) == 0 {
			return
		}

		parts = append(parts, string(current))
		current = current[:0]
	}

	for _, r := range value {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			if len(current) > 0 && unicode.IsUpper(r) && hasLower(current) {
				flush()
			}
			current = append(current, r)
		default:
			flush()
		}
	}
	flush()

	return parts
}

func hasLower(rs []rune) bool {
	for _, r := range rs {
		if unicode.IsLower(r) {
			return true
		}
	}

	return false
}

func serviceAssetVarName(serviceName string) string {
	goName := typeName(serviceName, "")
	if goName == "" {
		return "serviceWSDL"
	}

	rs := []rune(goName)
	rs[0] = unicode.ToLower(rs[0])
	return string(rs) + "WSDL"
}
