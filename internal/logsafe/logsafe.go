// Package logsafe cleans what a client sent before it goes in a log line.
package logsafe

import "strings"

// Line removes the line breaks of s, so that a client cannot forge a log entry.
func Line(s string) string {
	s = strings.ReplaceAll(s, "\n", "")
	return strings.ReplaceAll(s, "\r", "")
}
