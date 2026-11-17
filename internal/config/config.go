package config

import (
	"encoding/base64"
	"fmt"
	"net"
	"net/url"
	"os"
	"spartial.local/backend/internal/security"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"
)

type Config struct {
	IdentifierIndexKey       []byte
	TelemetryEndpoint        string
	MFAEnforceRemoteClinical bool
	PrivacyReadThreshold     int
	PrivacyMFAThreshold      int
	Env, Addr, DatabaseURL   string
	Origins                  []string
	CookieSecure             bool
	Timezone                 string
	// Keys encrypts MFA secrets (and later selected PHI fields). Nil means MFA enrollment is unavailable.
	Keys *security.Keyring
	// MFAEnforcePrivileged blocks privileged roles (administrator, privacy-reviewer) from protected
	// routes until their session has passed a second factor. Always on in production.
	MFAEnforcePrivileged  bool
	TrustedProxies        []string
	CarePolicyEnabled     bool
	CarePolicyApprovalRef string
	ClinicNetworks        []string
}

func Load() (Config, error) {
	c := Config{Env: get("APP_ENV", "development"), Addr: get("HTTP_ADDR", "127.0.0.1:8080"), DatabaseURL: os.Getenv("DATABASE_URL"), Timezone: get("CLINIC_TIMEZONE", "Africa/Dar_es_Salaam")}
	if c.Env != "development" && c.Env != "test" && c.Env != "production" {
		return c, fmt.Errorf("invalid APP_ENV")
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("DATABASE_URL is required")
	}
	u, err := url.Parse(c.DatabaseURL)
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") || u.Host == "" {
		return c, fmt.Errorf("invalid DATABASE_URL")
	}
	if c.Env == "production" {
		if u.Query().Get("sslmode") != "verify-full" || u.Query().Get("sslrootcert") == "" {
			return c, fmt.Errorf("production DATABASE_URL requires sslmode=verify-full and an explicit sslrootcert")
		}
		for _, entry := range strings.Split(u.Host, ",") {
			host := entry
			if h, _, e := net.SplitHostPort(entry); e == nil {
				host = h
			}
			if host == "" || strings.ContainsAny(host, " /\\") {
				return c, fmt.Errorf("production database host must be explicit")
			}
		}
		if values, ok := u.Query()["sslmode"]; ok && len(values) != 1 {
			return c, fmt.Errorf("duplicate database TLS settings are not allowed")
		}
	}
	for _, name := range []string{"TRUSTED_PROXIES", "CLINIC_NETWORKS"} {
		raw := os.Getenv(name)
		if raw == "" {
			continue
		}
		for _, entry := range strings.Split(raw, ",") {
			entry = strings.TrimSpace(entry)
			ip := net.ParseIP(entry)
			_, subnet, e := net.ParseCIDR(entry)
			if ip == nil && e != nil {
				return c, fmt.Errorf("%s must list explicit IP addresses or CIDR networks", name)
			}
			if subnet != nil {
				ones, _ := subnet.Mask.Size()
				if ones == 0 {
					return c, fmt.Errorf("%s must not trust all addresses", name)
				}
			}
			if name == "TRUSTED_PROXIES" {
				c.TrustedProxies = append(c.TrustedProxies, entry)
			} else {
				c.ClinicNetworks = append(c.ClinicNetworks, entry)
			}
		}
	}
	c.CarePolicyApprovalRef = os.Getenv("CARE_POLICY_APPROVAL_REF")
	enabled, e := strconv.ParseBool(get("CARE_POLICY_ENABLED", strconv.FormatBool(c.Env != "production")))
	if e != nil {
		return c, fmt.Errorf("CARE_POLICY_ENABLED must be true or false")
	}
	c.CarePolicyEnabled = enabled
	if c.Env == "production" && c.CarePolicyEnabled && strings.TrimSpace(c.CarePolicyApprovalRef) == "" {
		return c, fmt.Errorf("CARE_POLICY_APPROVAL_REF is required to enable production clinical access")
	}
	if _, _, err := net.SplitHostPort(c.Addr); err != nil {
		return c, fmt.Errorf("invalid HTTP_ADDR")
	}
	if _, err := time.LoadLocation(c.Timezone); err != nil {
		return c, fmt.Errorf("invalid CLINIC_TIMEZONE")
	}
	for _, o := range strings.Split(get("ALLOWED_ORIGINS", "http://localhost:3000"), ",") {
		o = strings.TrimSpace(o)
		u, e := url.Parse(o)
		if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return c, fmt.Errorf("ALLOWED_ORIGINS must contain exact origins")
		}
		if c.Env == "production" && u.Scheme != "https" {
			return c, fmt.Errorf("production origins must use HTTPS")
		}
		c.Origins = append(c.Origins, o)
	}
	c.TelemetryEndpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if c.TelemetryEndpoint != "" {
		u, e := url.Parse(c.TelemetryEndpoint)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || (c.Env == "production" && u.Scheme != "https") {
			return c, fmt.Errorf("telemetry endpoint must be an explicit HTTP(S) collector without credentials/query; production requires HTTPS")
		}
	}
	remote, e := strconv.ParseBool(get("MFA_ENFORCE_REMOTE_CLINICAL", strconv.FormatBool(c.Env == "production")))
	if e != nil {
		return c, fmt.Errorf("MFA_ENFORCE_REMOTE_CLINICAL must be true or false")
	}
	if c.Env == "production" && !remote {
		return c, fmt.Errorf("remote clinical MFA cannot be disabled in production")
	}
	c.MFAEnforceRemoteClinical = remote
	for _, name := range []string{"PRIVACY_READ_THRESHOLD", "PRIVACY_MFA_THRESHOLD"} {
		n, e := strconv.Atoi(get(name, "0"))
		if e != nil || n < 0 || n > 1000000 {
			return c, fmt.Errorf("privacy thresholds must be between zero (disabled) and 1000000")
		}
		if name == "PRIVACY_READ_THRESHOLD" {
			c.PrivacyReadThreshold = n
		} else {
			c.PrivacyMFAThreshold = n
		}
	}
	if raw := os.Getenv("IDENTIFIER_INDEX_KEY"); raw != "" {
		key, e := base64.StdEncoding.DecodeString(raw)
		if e != nil || len(key) != 32 {
			return c, fmt.Errorf("IDENTIFIER_INDEX_KEY must be 32 random bytes in base64")
		}
		c.IdentifierIndexKey = key
	}
	c.CookieSecure = c.Env != "development" && c.Env != "test"
	if spec := os.Getenv("PHI_ENCRYPTION_KEYS"); spec != "" {
		k, err := security.ParseKeyring(spec, os.Getenv("PHI_ACTIVE_KEY_ID"))
		if err != nil {
			return c, fmt.Errorf("invalid PHI_ENCRYPTION_KEYS/PHI_ACTIVE_KEY_ID: %w", err)
		}
		c.Keys = k
	} else if c.Env == "production" {
		return c, fmt.Errorf("PHI_ENCRYPTION_KEYS is required in production")
	}
	switch get("MFA_ENFORCE_PRIVILEGED", map[bool]string{true: "true", false: "false"}[c.Env == "production"]) {
	case "true":
		c.MFAEnforcePrivileged = true
	case "false":
		if c.Env == "production" {
			return c, fmt.Errorf("MFA_ENFORCE_PRIVILEGED cannot be disabled in production")
		}
	default:
		return c, fmt.Errorf("MFA_ENFORCE_PRIVILEGED must be true or false")
	}
	return c, nil
}
func get(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func (c Config) IsClinicIP(raw string) bool {
	ip := net.ParseIP(raw)
	if ip == nil {
		return false
	}
	for _, entry := range c.ClinicNetworks {
		if parsed := net.ParseIP(entry); parsed != nil && parsed.Equal(ip) {
			return true
		}
		if _, network, e := net.ParseCIDR(entry); e == nil && network.Contains(ip) {
			return true
		}
	}
	return false
}
