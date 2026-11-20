package util

import "strings"

// encodeUnreserved is the set JS encodeURIComponent leaves untouched:
// A-Z a-z 0-9 - _ . ! ~ * ' ( )
const encodeUnreserved = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_.!~*'()"

const hexUpper = "0123456789ABCDEF"

// EncodeURIComponent is the JS encodeURIComponent equivalent (Go's
// url.QueryEscape/PathEscape differ: they escape '~' etc. and use '+' for space).
// Each non-unreserved UTF-8 byte is percent-encoded with uppercase hex.
func EncodeURIComponent(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if strings.IndexByte(encodeUnreserved, c) >= 0 {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hexUpper[c>>4])
			b.WriteByte(hexUpper[c&0x0F])
		}
	}
	return b.String()
}
