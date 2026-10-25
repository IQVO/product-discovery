// Package shared holds the few value rules every domain package of
// product-discovery agrees on: what an opaque id looks like and the error a
// fact that can never be folded carries. It is pure: no I/O, no clock.
package shared

import "errors"

// ErrInvalidFact marks a consumed or snapshot fact that can never be folded
// (an id the projection cannot hold, a negative quantity, an unknown state).
// It is deterministic: retrying it changes nothing, so consumers dead-letter it.
var ErrInvalidFact = errors.New("invalid fact")

// MaxID is the longest accepted id.
const MaxID = 64

// ValidID reports whether s is an opaque id: 1 to MaxID characters from
// A-Z a-z 0-9 . _ : -, starting with a letter or a digit.
func ValidID(s string) bool {
	if s == "" || len(s) > MaxID {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isAlnum(c) {
			continue
		}
		if i > 0 && (c == '.' || c == '_' || c == ':' || c == '-') {
			continue
		}
		return false
	}
	return true
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}
