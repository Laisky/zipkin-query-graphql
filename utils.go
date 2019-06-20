package zipkin_graphql

import (
	"strings"
)

// URLJoin join path
func URLJoin(paths ...string) (p string) {
	l := len(paths) - 1
	if l == -1 {
		return ""
	}

	for i, pp := range paths {
		if i != l {
			p += strings.Trim(pp, "/") + "/"
		} else {
			p += strings.TrimLeft(pp, "/")
		}
	}

	return p
}
