package security

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// RFC 6238 Appendix B, SHA1 vectors (8 digits).
func TestHOTPMatchesRFC6238(t *testing.T) {
	secret := []byte("12345678901234567890")
	for _, v := range []struct {
		unix int64
		want string
	}{{59, "94287082"}, {1111111109, "07081804"}, {1111111111, "14050471"}, {1234567890, "89005924"}, {2000000000, "69279037"}, {20000000000, "65353130"}} {
		if got := HOTP(secret, v.unix/30, 8); got != v.want {
			t.Fatalf("T=%d: got %s want %s", v.unix, got, v.want)
		}
	}
}

func TestVerifyTOTPWindowAndReplay(t *testing.T) {
	secret, _ := NewTOTPSecret()
	now := time.Unix(1_800_000_000, 0)
	step := TOTPStep(now)
	code := HOTP(secret, step, TOTPDigits)
	got, ok := VerifyTOTP(secret, code, now, 0)
	if !ok || got != step {
		t.Fatal("current code rejected")
	}
	if _, ok = VerifyTOTP(secret, code, now, got); ok {
		t.Fatal("replayed code accepted")
	}
	if _, ok = VerifyTOTP(secret, HOTP(secret, step+1, TOTPDigits), now, 0); !ok {
		t.Fatal("next-step drift rejected")
	}
	if _, ok = VerifyTOTP(secret, HOTP(secret, step-2, TOTPDigits), now, 0); ok {
		t.Fatal("code outside window accepted")
	}
	for _, bad := range []string{"", "12345", "1234567", "abcdef"} {
		if _, ok = VerifyTOTP(secret, bad, now, 0); ok {
			t.Fatalf("malformed %q accepted", bad)
		}
	}
	if u := ProvisioningURI("Spartial", "a@b.test", secret); !strings.HasPrefix(u, "otpauth://totp/Spartial:a@b.test?") || !strings.Contains(u, "secret="+EncodeTOTPSecret(secret)) {
		t.Fatal(u)
	}
}

func TestRecoveryCodes(t *testing.T) {
	c, e := NewRecoveryCode()
	if e != nil || len(c) != 19 {
		t.Fatal(c, e)
	}
	n := NormalizeRecoveryCode(" " + strings.ToLower(c) + " ")
	if n == "" || n != strings.ReplaceAll(c, "-", "") {
		t.Fatal("normalization failed")
	}
	if NormalizeRecoveryCode("short") != "" || NormalizeRecoveryCode("0000-0000-0000-0000") != "" {
		t.Fatal("malformed code accepted")
	}
}

func TestKeyring(t *testing.T) {
	k1, _ := NewKeyMaterial()
	k2, _ := NewKeyMaterial()
	old, e := ParseKeyring("k1:"+k1, "k1")
	if e != nil {
		t.Fatal(e)
	}
	ct, e := old.Seal([]byte("secret"), []byte("staff_mfa:1"))
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(ct, []byte("secret")) {
		t.Fatal("plaintext visible")
	}
	rotated, e := ParseKeyring("k1:"+k1+",k2:"+k2, "k2")
	if e != nil {
		t.Fatal(e)
	}
	if pt, e := rotated.Open(ct, []byte("staff_mfa:1")); e != nil || string(pt) != "secret" {
		t.Fatal("rotation lost old ciphertext")
	}
	if _, e = rotated.Open(ct, []byte("staff_mfa:2")); e == nil {
		t.Fatal("ciphertext moved to another row was accepted")
	}
	tampered := append([]byte{}, ct...)
	tampered[len(tampered)-1] ^= 1
	if _, e = rotated.Open(tampered, []byte("staff_mfa:1")); e == nil {
		t.Fatal("tampered ciphertext accepted")
	}
	if _, e = ParseKeyring("k2:"+k2, "k2"); e != nil {
		t.Fatal(e)
	} else if only2, _ := ParseKeyring("k2:"+k2, "k2"); func() bool { _, e := only2.Open(ct, []byte("staff_mfa:1")); return e == nil }() {
		t.Fatal("retired key still decrypts")
	}
	for _, spec := range []string{"", "k1", "K1:" + k1, "k1:short", "k1:" + k1 + ",k1:" + k2} {
		if _, e = ParseKeyring(spec, "k1"); e == nil {
			t.Fatalf("invalid keyring %q accepted", spec)
		}
	}
	if _, e = ParseKeyring("k1:"+k1, "k9"); e == nil {
		t.Fatal("missing active key accepted")
	}
}
