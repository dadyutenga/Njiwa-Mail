package api

import "crypto/subtle"

func subtleCompare(got, want string) int {
	return subtle.ConstantTimeCompare([]byte(got), []byte(want))
}
