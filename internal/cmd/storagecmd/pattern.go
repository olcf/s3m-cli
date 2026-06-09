package storagecmd

import "strings"

//
// Pattern detection

func isGlobPattern(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}
