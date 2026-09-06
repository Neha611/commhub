package app

import "strings"

func splitLines(s string) []string { return strings.Split(s, "\n") }
func trimSpace(s string) string    { return strings.TrimSpace(s) }
