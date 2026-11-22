package mcp

import "strings"

// sanitizeLog strips CR and LF from a request-derived value (an error's text)
// before it is logged, so the value cannot start a forged log line (CWE-117,
// CodeQL go/log-injection). It uses strings.ReplaceAll: this package's
// write-seam sensor forbids the bare identifier of a replacer's method.
func sanitizeLog(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r", ""), "\n", "")
}
