package config

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strings"
	"time"
	_ "time/tzdata"
)

type Config struct {
	Env, Addr, DatabaseURL string
	Origins                []string
	CookieSecure           bool
	Timezone               string
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
	c.CookieSecure = c.Env != "development" && c.Env != "test"
	return c, nil
}
func get(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
