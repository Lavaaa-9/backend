package security

import "testing"

func TestCommonPasswordPolicy(t *testing.T) {
	for _, p := range []string{"password", "123456789012", "passwordpassword", "PASSWORDPASSWORD"} {
		if PasswordAllowed(p) {
			t.Fatal("common password accepted")
		}
	}
	if !PasswordAllowed("Synthetic-test-password-234!") {
		t.Fatal("strong synthetic password rejected")
	}
}
