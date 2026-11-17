package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP follows RFC 6238 with the parameters every authenticator app supports:
// HMAC-SHA1, 30-second steps, 6 digits. SHA1 is acceptable here because HOTP relies on
// HMAC's PRF property, not collision resistance (RFC 4226 §B, NIST SP 800-63B §5.1.4).
const (
	TOTPPeriod = 30
	TOTPDigits = 6
	// Accept one step either side for clock drift; never more.
	TOTPSkew = 1
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewTOTPSecret returns a 160-bit secret (RFC 4226 recommended length).
func NewTOTPSecret() ([]byte, error) {
	s := make([]byte, 20)
	_, e := rand.Read(s)
	return s, e
}

func EncodeTOTPSecret(secret []byte) string { return b32.EncodeToString(secret) }

func TOTPStep(t time.Time) int64 { return t.Unix() / TOTPPeriod }

// HOTP computes RFC 4226 for a counter with the requested number of digits.
func HOTP(secret []byte, counter int64, digits int) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(counter))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	code := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	mod := uint32(1)
	for i := 0; i < digits; i++ {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", digits, code%mod)
}

// VerifyTOTP checks code against the window around now and refuses any step at or below
// lastStep, so an intercepted or shoulder-surfed code cannot be replayed.
// It returns the matched step, which the caller must persist as the new lastStep.
func VerifyTOTP(secret []byte, code string, now time.Time, lastStep int64) (int64, bool) {
	code = strings.TrimSpace(code)
	if len(code) != TOTPDigits {
		return 0, false
	}
	current := TOTPStep(now)
	matched, ok := int64(0), false
	// Check every candidate without early exit to keep timing independent of the match.
	for d := int64(-TOTPSkew); d <= TOTPSkew; d++ {
		step := current + d
		if step <= lastStep {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(HOTP(secret, step, TOTPDigits)), []byte(code)) == 1 && !ok {
			matched, ok = step, true
		}
	}
	return matched, ok
}

// ProvisioningURI is the otpauth:// URI rendered as a QR code by the client.
func ProvisioningURI(issuer, account string, secret []byte) string {
	v := url.Values{}
	v.Set("secret", EncodeTOTPSecret(secret))
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", fmt.Sprint(TOTPDigits))
	v.Set("period", fmt.Sprint(TOTPPeriod))
	return "otpauth://totp/" + url.PathEscape(issuer+":"+account) + "?" + v.Encode()
}

// Recovery codes: 80 random bits as 16 base32 characters shown as XXXX-XXXX-XXXX-XXXX.
// High entropy means a fast digest is sufficient for storage (unlike passwords).
func NewRecoveryCode() (string, error) {
	b := make([]byte, 10)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	s := b32.EncodeToString(b)
	return s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16], nil
}

// NormalizeRecoveryCode accepts any case and optional separators; returns "" if malformed.
func NormalizeRecoveryCode(code string) string {
	r := strings.NewReplacer("-", "", " ", "")
	c := strings.ToUpper(r.Replace(strings.TrimSpace(code)))
	if len(c) != 16 {
		return ""
	}
	if _, e := b32.DecodeString(c); e != nil {
		return ""
	}
	return c
}
