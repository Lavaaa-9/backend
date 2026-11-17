package config

import "testing"

func TestConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://synthetic:synthetic@localhost:5432/synthetic")
	t.Setenv("APP_ENV", "development")
	t.Setenv("ALLOWED_ORIGINS", "http://localhost:3000")
	t.Setenv("HTTP_ADDR", "127.0.0.1:8080")
	t.Setenv("CLINIC_TIMEZONE", "Africa/Dar_es_Salaam")
	c, e := Load()
	if e != nil || c.CookieSecure {
		t.Fatalf("development configuration: %v", e)
	}
	t.Run("production secure cookies", func(t *testing.T) {
		t.Setenv("APP_ENV", "production")
		t.Setenv("ALLOWED_ORIGINS", "https://clinic.example")
		c, e := Load()
		if e != nil || !c.CookieSecure {
			t.Fatalf("production cookies: %v", e)
		}
	})
	for _, tc := range []struct{ name, key, value string }{{"wildcard", "ALLOWED_ORIGINS", "*"}, {"origin path", "ALLOWED_ORIGINS", "https://clinic.example/path"}, {"origin credentials", "ALLOWED_ORIGINS", "https://user@clinic.example"}, {"unknown timezone", "CLINIC_TIMEZONE", "not-a-timezone"}, {"bad listener", "HTTP_ADDR", "localhost"}, {"missing database", "DATABASE_URL", ""}, {"wrong database scheme", "DATABASE_URL", "https://example.com"}, {"unknown environment", "APP_ENV", "prod-ish"}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, e := Load(); e == nil {
				t.Fatal("invalid configuration accepted")
			}
		})
	}
}
