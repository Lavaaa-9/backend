package config

import (
	"strings"
	"testing"
)

const testKey = "k1:AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="

func TestConfiguration(t *testing.T) {
	t.Setenv("PHI_ENCRYPTION_KEYS", "")
	t.Setenv("PHI_ACTIVE_KEY_ID", "")
	t.Setenv("MFA_ENFORCE_PRIVILEGED", "")
	t.Setenv("TRUSTED_PROXIES", "")
	t.Setenv("CLINIC_NETWORKS", "")
	t.Setenv("CARE_POLICY_ENABLED", "")
	t.Setenv("CARE_POLICY_APPROVAL_REF", "")
	t.Setenv("DATABASE_URL", "postgres://synthetic:synthetic@localhost:5432/synthetic")
	t.Setenv("APP_ENV", "development")
	t.Setenv("ALLOWED_ORIGINS", "http://localhost:3000")
	t.Setenv("HTTP_ADDR", "127.0.0.1:8080")
	t.Setenv("CLINIC_TIMEZONE", "Africa/Dar_es_Salaam")
	c, e := Load()
	if e != nil || c.CookieSecure {
		t.Fatalf("development configuration: %v", e)
	}
	if c.Keys != nil || c.MFAEnforcePrivileged {
		t.Fatal("development defaults changed unexpectedly")
	}
	t.Run("production secure cookies keys and MFA", func(t *testing.T) {
		t.Setenv("APP_ENV", "production")
		t.Setenv("DATABASE_URL", "postgres://synthetic:synthetic@database.example:5432/synthetic?sslmode=verify-full&sslrootcert=/etc/clinic/ca.pem")
		t.Setenv("ALLOWED_ORIGINS", "https://clinic.example")
		if _, e := Load(); e == nil || !strings.Contains(e.Error(), "PHI_ENCRYPTION_KEYS") {
			t.Fatalf("production without keys accepted: %v", e)
		}
		t.Setenv("PHI_ENCRYPTION_KEYS", testKey)
		t.Setenv("PHI_ACTIVE_KEY_ID", "k1")
		c, e := Load()
		if e != nil || !c.CookieSecure || c.Keys == nil || !c.MFAEnforcePrivileged {
			t.Fatalf("production configuration: %v", e)
		}
		t.Setenv("MFA_ENFORCE_PRIVILEGED", "false")
		if _, e := Load(); e == nil {
			t.Fatal("production MFA could be disabled")
		}
	})
	t.Run("development opt-in MFA enforcement", func(t *testing.T) {
		t.Setenv("MFA_ENFORCE_PRIVILEGED", "true")
		t.Setenv("PHI_ENCRYPTION_KEYS", testKey)
		t.Setenv("PHI_ACTIVE_KEY_ID", "k1")
		c, e := Load()
		if e != nil || !c.MFAEnforcePrivileged || c.Keys == nil {
			t.Fatal(e)
		}
	})
	for _, tc := range []struct{ name, key, value string }{{"wildcard", "ALLOWED_ORIGINS", "*"}, {"origin path", "ALLOWED_ORIGINS", "https://clinic.example/path"}, {"origin credentials", "ALLOWED_ORIGINS", "https://user@clinic.example"}, {"unknown timezone", "CLINIC_TIMEZONE", "not-a-timezone"}, {"bad listener", "HTTP_ADDR", "localhost"}, {"missing database", "DATABASE_URL", ""}, {"wrong database scheme", "DATABASE_URL", "https://example.com"}, {"unknown environment", "APP_ENV", "prod-ish"}, {"malformed key", "PHI_ENCRYPTION_KEYS", "k1:short"}, {"bad MFA flag", "MFA_ENFORCE_PRIVILEGED", "yes"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, e := Load(); e == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}

func TestTransportAndCareActivation(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://synthetic:synthetic@database.example:5432/synthetic?sslmode=disable")
	t.Setenv("APP_ENV", "production")
	t.Setenv("ALLOWED_ORIGINS", "https://clinic.example")
	t.Setenv("PHI_ENCRYPTION_KEYS", testKey)
	t.Setenv("PHI_ACTIVE_KEY_ID", "k1")
	t.Setenv("MFA_ENFORCE_PRIVILEGED", "true")
	t.Setenv("MFA_ENFORCE_REMOTE_CLINICAL", "true")
	t.Setenv("CARE_POLICY_ENABLED", "false")
	t.Setenv("TRUSTED_PROXIES", "")
	if _, e := Load(); e == nil {
		t.Fatal("plaintext production database accepted")
	}
	t.Setenv("DATABASE_URL", "postgres://synthetic:synthetic@database.example:5432/synthetic?sslmode=verify-full&sslrootcert=/etc/clinic/ca.pem")
	for _, raw := range []string{"*", "0.0.0.0/0", "::/0", "not-an-address"} {
		t.Setenv("TRUSTED_PROXIES", raw)
		if _, e := Load(); e == nil {
			t.Fatal("unrestricted/invalid proxy accepted")
		}
	}
	t.Setenv("TRUSTED_PROXIES", "127.0.0.1,10.20.0.0/16")
	t.Setenv("CLINIC_NETWORKS", "192.168.30.0/24")
	c, e := Load()
	if e != nil || len(c.TrustedProxies) != 2 || !c.IsClinicIP("192.168.30.3") || c.IsClinicIP("203.0.113.10") {
		t.Fatalf("explicit networks: %v", e)
	}
	t.Setenv("CARE_POLICY_ENABLED", "true")
	t.Setenv("CARE_POLICY_APPROVAL_REF", "")
	if _, e := Load(); e == nil {
		t.Fatal("unapproved production care policy enabled")
	}
	t.Setenv("CARE_POLICY_APPROVAL_REF", "synthetic-test-approval-record")
	if _, e := Load(); e != nil {
		t.Fatal(e)
	}
}
