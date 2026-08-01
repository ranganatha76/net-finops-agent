package azure

import "strings"

// contains reports whether s contains sub as a substring.
func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
