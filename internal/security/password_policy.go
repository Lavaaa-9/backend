package security

import (
	"embed"
	"strings"
	"unicode/utf8"
)

//go:embed passwords/common.txt passwords/supplemental.txt
var passwordDictionary embed.FS
var commonPasswords = func() map[string]struct{} {
	data, _ := passwordDictionary.ReadFile("passwords/common.txt")
	extra, _ := passwordDictionary.ReadFile("passwords/supplemental.txt")
	data = append(data, extra...)
	out := map[string]struct{}{}
	for _, p := range strings.Split(string(data), "\n") {
		if p != "" {
			out[strings.ToLower(strings.TrimSpace(p))] = struct{}{}
		}
	}
	return out
}()

// PasswordAllowed performs an offline common-password screen; it is not an exhaustive breach check.
func PasswordAllowed(password string) bool {
	n := utf8.RuneCountInString(password)
	if n < 12 || n > 256 {
		return false
	}
	_, blocked := commonPasswords[strings.ToLower(strings.TrimSpace(password))]
	return !blocked
}
