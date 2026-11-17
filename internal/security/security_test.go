package security

import "testing"

func TestPasswordHash(t *testing.T) {
	h, e := HashPassword("long-enough-test-password")
	if e != nil {
		t.Fatal(e)
	}
	if !VerifyPassword(h, "long-enough-test-password") || VerifyPassword(h, "wrong") {
		t.Fatal("password verification failed")
	}
	for _, h := range []string{"", "$argon2id$v=19$m=999999999,t=100,p=8$bad$bad", "$argon2id$v=19$m=65536,t=3,p=2$bad$bad"} {
		if VerifyPassword(h, "password") {
			t.Fatal("invalid hash accepted")
		}
	}
}
func TestTokens(t *testing.T) {
	a, e := Token()
	if e != nil {
		t.Fatal(e)
	}
	b, e := Token()
	if e != nil {
		t.Fatal(e)
	}
	if len(a) != 43 || a == b || len(Digest(a)) != 64 {
		t.Fatal("invalid token entropy/encoding")
	}
}
