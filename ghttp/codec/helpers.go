package codec

import (
	"strings"
)

func normalizeContentType(ct string) string {
	ct = strings.TrimSpace(strings.ToLower(ct))
	if ct == "" {
		return ct
	}
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = ct[:idx]
	}
	return ct
}

func matchContentType(target string, candidates []string) bool {
	if target == "" {
		return false
	}
	t := normalizeContentType(target)
	for _, c := range candidates {
		if t == normalizeContentType(c) {
			return true
		}
	}
	return false
}
