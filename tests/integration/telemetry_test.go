package integration

import (
	"context"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"net/http"
	"net/http/httptest"
	"spartial.local/backend/internal/config"
	"spartial.local/backend/internal/modules/privacy"
	"spartial.local/backend/internal/security"
	"strings"
	"testing"
)

func TestTelemetryContainsOnlyAllowlistedMetadata(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(provider)
	defer otel.SetTracerProvider(previous)
	defer provider.Shutdown(context.Background())
	f := setup(t)
	reception := token(f.login(t, "reception@spartial.test"))
	admin := token(f.login(t, "admin@spartial.test"))
	p := f.request(t, "POST", "/api/v1/staff/patients", reception, f.patientInput(), 201, map[string]string{"Idempotency-Key": "telemetry-patient"})
	pid := p.body["id"].(string)
	f.request(t, "GET", "/api/v1/staff/patients/"+pid+"?q=SensitiveSyntheticName", reception, nil, 200, nil)
	req, e := f.call("GET", "/api/v1/staff/metrics", reception, nil, nil)
	if e == nil && req.status != 403 {
		t.Fatal("unprivileged metrics access accepted")
	}
	// This endpoint is text, so use an ordinary HTTP test response for the authorized scrape.
	r, e := f.app.Test(newRequest("GET", "/api/v1/staff/metrics", admin))
	if e != nil {
		t.Fatal(e)
	}
	defer r.Body.Close()
	if r.StatusCode != 200 {
		t.Fatal("authorized metrics scrape failed")
	}
	spans := exporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("no spans recorded")
	}
	for _, span := range spans {
		for _, attribute := range span.Attributes {
			switch string(attribute.Key) {
			case "http.request.method", "http.route", "http.response.status_code":
			default:
				t.Fatalf("unexpected telemetry attribute %s", attribute.Key)
			}
			value := attribute.Value.Emit()
			if strings.Contains(value, pid) || strings.Contains(value, "SensitiveSyntheticName") || strings.Contains(value, reception) {
				t.Fatal("sensitive value exported in telemetry")
			}
		}
	}
}
func TestTrustedProxyRateLimitIdentity(t *testing.T) {
	for _, tc := range []struct {
		name    string
		proxies []string
		ip      string
	}{{"direct peer ignores spoof", nil, "0.0.0.0"}, {"trusted peer uses validated forwarding chain", []string{"0.0.0.0"}, "203.0.113.24"}} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupWith(t, config.Config{Env: "test", Origins: []string{"http://localhost:3000"}, TrustedProxies: tc.proxies})
			ctx := context.Background()
			_, e := f.owner.Exec(ctx, `INSERT INTO auth_limits(bucket,window_start,attempts) VALUES($1,now(),10)`, "ip:"+security.Digest(tc.ip))
			if e != nil {
				t.Fatal(e)
			}
			f.request(t, "POST", "/api/v1/auth/login", "", map[string]any{"email": "admin@spartial.test", "password": password, "client_type": "mobile"}, 429, map[string]string{"X-Forwarded-For": "192.0.2.11, 203.0.113.24"})
		})
	}
}
func TestPrivacyAlertDeduplication(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	r := f.login(t, "reception@spartial.test")
	actor := r.body["user"].(map[string]any)["id"].(string)
	_, e := f.owner.Exec(ctx, `INSERT INTO audit_events(actor_id,action,request_id,outcome) SELECT $1::uuid,'patients.read',gen_random_uuid(),'success' FROM generate_series(1,3)`, actor)
	if e != nil {
		t.Fatal(e)
	}
	for i := 0; i < 2; i++ {
		if e = privacy.DetectAlerts(ctx, f.runtime, 2, 0); e != nil {
			t.Fatal(e)
		}
	}
	var n, count int
	if e = f.owner.QueryRow(ctx, `SELECT count(*),max(event_count) FROM privacy_alerts WHERE actor_id=$1::uuid`, actor).Scan(&n, &count); e != nil || n != 1 || count != 3 {
		t.Fatalf("alerts duplicated or miscounted n=%d count=%d error=%v", n, count, e)
	}
}

func newRequest(method, path, token string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func TestRemoteClinicalMFAPolicy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		networks []string
		expected int
	}{{"remote doctor requires MFA", nil, 403}, {"explicit clinic network permits password assurance", []string{"0.0.0.0/32"}, 200}} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupWith(t, config.Config{Env: "test", Origins: []string{"http://localhost:3000"}, MFAEnforceRemoteClinical: true, ClinicNetworks: tc.networks})
			admin := token(f.login(t, "admin@spartial.test"))
			f.request(t, "POST", "/api/v1/staff/users", admin, map[string]any{"email": "remote@spartial.test", "display_name": "Synthetic remote doctor", "password": password, "role": "doctor", "facility_ids": []string{f.facility}}, 201, nil)
			doctor := token(f.login(t, "remote@spartial.test"))
			f.request(t, "GET", "/api/v1/staff/facilities", doctor, nil, tc.expected, nil)
		})
	}
}

func TestProductionTransportIgnoresUntrustedScheme(t *testing.T) {
	for _, tc := range []struct {
		name     string
		proxies  []string
		expected int
	}{{"untrusted scheme spoof rejected", nil, 426}, {"explicit TLS proxy accepted", []string{"0.0.0.0"}, 200}} {
		t.Run(tc.name, func(t *testing.T) {
			f := setupWith(t, config.Config{Env: "production", Origins: []string{"https://clinic.example"}, TrustedProxies: tc.proxies})
			f.request(t, "GET", "/api/openapi.json", "", nil, tc.expected, map[string]string{"X-Forwarded-Proto": "https"})
		})
	}
}
