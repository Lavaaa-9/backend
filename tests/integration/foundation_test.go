package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	urlpkg "net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/config"
	"spartial.local/backend/internal/httpapi"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
)

const password = "Synthetic-test-password-234!"

type fixture struct {
	app            *fiber.App
	owner, runtime *pgxpool.Pool
	facility       string
}

func setup(t *testing.T) *fixture {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is required for real PostgreSQL integration tests")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	suffix := strings.ReplaceAll(uuid.NewString(), "-", "")
	name := "spartial_test_" + suffix
	role := "spartial_app_" + suffix
	if _, e = admin.Exec(ctx, `CREATE DATABASE `+name); e != nil {
		admin.Close()
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_, e := admin.Exec(ctx, `DROP DATABASE `+name+` WITH (FORCE)`)
		if e != nil {
			t.Error(e)
		}
		_, e = admin.Exec(ctx, `DROP ROLE IF EXISTS `+role)
		if e != nil {
			t.Error(e)
		}
		admin.Close()
	})
	ownerURLValue, e := urlpkg.Parse(url)
	if e != nil {
		t.Fatal(e)
	}
	ownerURLValue.Path = "/" + name
	ownerURL := ownerURLValue.String()
	if e = platform.Migrate(ctx, ownerURL); e != nil {
		t.Fatal(e)
	}
	owner, e := platform.Open(ctx, ownerURL)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(owner.Close)
	if _, e = owner.Exec(ctx, `CREATE ROLE `+role+` LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE PASSWORD 'isolated-test-password-293847'`); e != nil {
		t.Fatal(e)
	}
	if e = platform.GrantRuntime(ctx, owner, role); e != nil {
		t.Fatal(e)
	}
	runtimeURL := *ownerURLValue
	runtimeURL.User = urlpkg.UserPassword(role, "isolated-test-password-293847")
	runtime, e := platform.Open(ctx, runtimeURL.String())
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(runtime.Close)
	facility, e := identity.Seed(ctx, owner, password, password)
	if e != nil {
		t.Fatal(e)
	}
	app, e := httpapi.New(config.Config{Env: "test", Origins: []string{"http://localhost:3000"}}, runtime, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if e != nil {
		t.Fatal(e)
	}
	return &fixture{app, owner, runtime, facility}
}

type response struct {
	status int
	body   map[string]any
	cookie string
}

func (f *fixture) call(method, path, token string, body any, headers map[string]string) (response, error) {
	var data []byte
	var e error
	if body != nil {
		data, e = json.Marshal(body)
		if e != nil {
			return response{}, e
		}
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	r, e := f.app.Test(req, fiber.TestConfig{Timeout: 30 * time.Second})
	if e != nil {
		return response{}, e
	}
	defer r.Body.Close()
	out := response{status: r.StatusCode, body: map[string]any{}, cookie: r.Header.Get("Set-Cookie")}
	if r.StatusCode != 204 {
		if e = json.NewDecoder(r.Body).Decode(&out.body); e != nil {
			return out, e
		}
	}
	return out, nil
}
func (f *fixture) request(t *testing.T, method, path, token string, body any, status int, headers map[string]string) response {
	t.Helper()
	r, e := f.call(method, path, token, body, headers)
	if e != nil {
		t.Fatal(e)
	}
	if r.status != status {
		t.Fatalf("%s %s: got %d, want %d: %v", method, path, r.status, status, r.body)
	}
	return r
}
func (f *fixture) login(t *testing.T, email string) response {
	t.Helper()
	return f.request(t, "POST", "/api/v1/auth/login", "", map[string]any{"email": email, "password": password, "client_type": "mobile"}, 200, nil)
}
func token(r response) string { return r.body["access_token"].(string) }
func demographic() map[string]any {
	return map[string]any{"given_name": "Synthetic", "family_name": "Patient", "birth_precision": "unknown", "phone": "+255000000000"}
}
func (f *fixture) patientInput() map[string]any {
	in := demographic()
	in["facility_id"] = f.facility
	return in
}
func TestFoundation(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	f.request(t, "GET", "/health/live", "", nil, 200, nil)
	f.request(t, "GET", "/health/ready", "", nil, 200, nil)
	f.request(t, "GET", "/api/v1/staff/patients", "", nil, 401, nil)
	admin := f.login(t, "admin@spartial.test")
	reception := f.login(t, "reception@spartial.test")
	at, rt := token(admin), token(reception)
	t.Run("authorization and strict DTOs", func(t *testing.T) {
		f.request(t, "GET", "/api/v1/staff/users", rt, nil, 403, nil)
		f.request(t, "GET", "/api/v1/staff/patients", at, nil, 403, nil)
		f.request(t, "GET", "/api/v1/staff/facilities", rt, nil, 200, nil)
		in := f.patientInput()
		in["role"] = "administrator"
		f.request(t, "POST", "/api/v1/staff/patients", rt, in, 400, map[string]string{"Idempotency-Key": "unknown-fields"})
		in = f.patientInput()
		in["given_name"] = ""
		f.request(t, "POST", "/api/v1/staff/patients", rt, in, 422, map[string]string{"Idempotency-Key": "invalid-name"})
		f.request(t, "POST", "/api/v1/staff/patients", rt, f.patientInput(), 422, nil)
		f.request(t, "GET", "/api/v1/staff/patients?limit=1000", rt, nil, 422, nil)
		f.request(t, "GET", "/api/v1/staff/patients/not-uuid", rt, nil, 422, nil)
	})
	var patientID, mrn string
	t.Run("concurrent idempotent registration shared phones and unique MRNs", func(t *testing.T) {
		const n = 8
		results := make(chan response, n)
		errs := make(chan error, n)
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, e := f.call("POST", "/api/v1/staff/patients", rt, f.patientInput(), map[string]string{"Idempotency-Key": "concurrent-register"})
				results <- r
				errs <- e
			}()
		}
		wg.Wait()
		close(results)
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		for r := range results {
			if r.status != 201 {
				t.Fatalf("registration status %d: %v", r.status, r.body)
			}
			id := r.body["id"].(string)
			if patientID != "" && id != patientID {
				t.Fatal("idempotency created multiple patients")
			}
			patientID = id
			mrn = r.body["mrn"].(string)
			if !strings.HasSuffix(r.body["created_at"].(string), "Z") {
				t.Fatal("timestamp was not encoded as UTC")
			}

		}
		p2 := f.request(t, "POST", "/api/v1/staff/patients", rt, f.patientInput(), 201, map[string]string{"Idempotency-Key": "shared-phone"})
		if p2.body["mrn"] == mrn {
			t.Fatal("MRN collision")
		}
		changed := f.patientInput()
		changed["given_name"] = "Different"
		f.request(t, "POST", "/api/v1/staff/patients", rt, changed, 409, map[string]string{"Idempotency-Key": "concurrent-register"})
		var patients, jobs int
		if e := f.owner.QueryRow(ctx, `SELECT count(*) FROM patients`).Scan(&patients); e != nil {
			t.Fatal(e)
		}
		if e := f.owner.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&jobs); e != nil {
			t.Fatal(e)
		}
		if patients != 2 || jobs != 2 {
			t.Fatalf("transaction effects patients=%d jobs=%d", patients, jobs)
		}
		f.request(t, "GET", "/api/v1/staff/patients?q=Synthetic", rt, nil, 200, nil)
	})
	t.Run("audit failure rolls back every registration effect", func(t *testing.T) {
		var role string
		if e := f.runtime.QueryRow(ctx, `SELECT current_user`).Scan(&role); e != nil {
			t.Fatal(e)
		}
		roleSQL := pgx.Identifier{role}.Sanitize()
		if _, e := f.owner.Exec(ctx, `REVOKE INSERT ON audit_events FROM `+roleSQL); e != nil {
			t.Fatal(e)
		}
		defer func() {
			if _, e := f.owner.Exec(ctx, `GRANT INSERT ON audit_events TO `+roleSQL); e != nil {
				t.Error(e)
			}
		}()
		in := f.patientInput()
		in["given_name"] = "Rollback-only"
		f.request(t, "POST", "/api/v1/staff/patients", rt, in, 500, map[string]string{"Idempotency-Key": "rollback-audit-test"})
		for _, query := range []string{`SELECT count(*) FROM patients WHERE given_name='Rollback-only'`, `SELECT count(*) FROM idempotency_records WHERE key='rollback-audit-test'`, `SELECT count(*) FROM outbox o LEFT JOIN patients p ON p.id=o.aggregate_id WHERE p.id IS NULL`} {
			var n int
			if e := f.owner.QueryRow(ctx, query).Scan(&n); e != nil {
				t.Fatal(e)
			}
			if n != 0 {
				t.Fatalf("registration side effect escaped rollback: %s", query)
			}
		}
	})
	t.Run("versioned demographic updates and birth precision", func(t *testing.T) {
		f.request(t, "GET", "/api/v1/staff/patients/"+patientID, rt, nil, 200, nil)
		in := demographic()
		in["version"] = 1
		in["birth_precision"] = "year"
		in["birth_year"] = 1990
		p := f.request(t, "PUT", "/api/v1/staff/patients/"+patientID, rt, in, 200, nil)
		if p.body["version"] != float64(2) {
			t.Fatal(p.body)
		}
		f.request(t, "PUT", "/api/v1/staff/patients/"+patientID, rt, in, 409, nil)
		in["version"] = 2
		in["birth_date"] = "1990-01-01"
		f.request(t, "PUT", "/api/v1/staff/patients/"+patientID, rt, in, 422, nil)
		in["birth_precision"] = "exact"
		delete(in, "birth_year")
		in["birth_date"] = "1990-04-18"
		exact := f.request(t, "PUT", "/api/v1/staff/patients/"+patientID, rt, in, 200, nil)
		if exact.body["birth_date"] != "1990-04-18" {
			t.Fatalf("date-only value changed: %v", exact.body)
		}
		in["version"] = 3
		in["birth_date"] = "2990-04-18"
		f.request(t, "PUT", "/api/v1/staff/patients/"+patientID, rt, in, 422, nil)

		f.request(t, "GET", "/api/v1/staff/patients?q=%27%25%3B%20DROP%20TABLE%20patients", rt, nil, 200, nil)
	})
	var secondID, secondToken string
	t.Run("cross facility isolation", func(t *testing.T) {
		f2 := f.request(t, "POST", "/api/v1/staff/facilities", at, map[string]any{"name": "Second Synthetic Facility", "timezone": "Africa/Dar_es_Salaam"}, 201, nil)
		in := map[string]any{"email": "second@spartial.test", "display_name": "Second Receptionist", "password": password, "role": "reception", "facility_ids": []any{f2.body["id"]}}
		u := f.request(t, "POST", "/api/v1/staff/users", at, in, 201, nil)
		secondID = u.body["id"].(string)
		secondToken = token(f.login(t, "second@spartial.test"))
		f.request(t, "GET", "/api/v1/staff/patients/"+patientID, secondToken, nil, 404, nil)
		edit := demographic()
		edit["version"] = 3
		f.request(t, "PUT", "/api/v1/staff/patients/"+patientID, secondToken, edit, 404, nil)
		f.request(t, "POST", "/api/v1/staff/patients", secondToken, f.patientInput(), 403, map[string]string{"Idempotency-Key": "cross-facility"})
		list := f.request(t, "GET", "/api/v1/staff/patients", secondToken, nil, 200, nil)
		if len(list.body["items"].([]any)) != 0 {
			t.Fatal("cross facility list leaked patients")
		}
	})
	t.Run("audit is durable append only and app cannot change permissions", func(t *testing.T) {
		f.request(t, "GET", "/api/v1/staff/audit", at, nil, 200, nil)
		f.request(t, "GET", "/api/v1/staff/audit", rt, nil, 403, nil)
		for _, q := range []string{`UPDATE audit_events SET outcome='failure'`, `DELETE FROM audit_events`, `TRUNCATE audit_events`, `INSERT INTO role_permissions VALUES('reception','staff.manage')`} {
			if _, e := f.runtime.Exec(ctx, q); e == nil {
				t.Fatal("runtime role unexpectedly allowed " + q)
			}
		}
		if _, e := f.owner.Exec(ctx, `UPDATE audit_events SET outcome='failure'`); e == nil {
			t.Fatal("audit trigger did not reject owner mutation")
		}
		var n int
		if e := f.owner.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE patient_id=$1::uuid`, patientID).Scan(&n); e != nil {
			t.Fatal(e)
		}
		if n < 3 {
			t.Fatal("missing patient audit events")
		}
	})
	t.Run("refresh rotation reuse detection revokes the family", func(t *testing.T) {
		old := reception.body["refresh_token"].(string)
		refreshed := f.request(t, "POST", "/api/v1/auth/refresh", "", map[string]any{"client_type": "mobile", "refresh_token": old}, 200, nil)
		f.request(t, "GET", "/api/v1/auth/me", rt, nil, 401, nil)
		f.request(t, "GET", "/api/v1/auth/me", token(refreshed), nil, 200, nil)
		f.request(t, "POST", "/api/v1/auth/refresh", "", map[string]any{"client_type": "mobile", "refresh_token": old}, 401, nil)
		f.request(t, "GET", "/api/v1/auth/me", token(refreshed), nil, 401, nil)
		f.request(t, "POST", "/api/v1/auth/refresh", "", map[string]any{"client_type": "mobile", "refresh_token": refreshed.body["refresh_token"]}, 401, nil)
	})
	t.Run("suspension reactivation and privilege changes revoke sessions", func(t *testing.T) {
		in := map[string]any{"display_name": "Second Receptionist", "role": "reception", "active": false, "facility_ids": []any{f.facility}, "version": 1}
		f.request(t, "PUT", "/api/v1/staff/users/"+secondID, at, in, 200, nil)
		f.request(t, "GET", "/api/v1/auth/me", secondToken, nil, 401, nil)
		f.request(t, "POST", "/api/v1/auth/login", "", map[string]any{"email": "second@spartial.test", "password": password, "client_type": "mobile"}, 401, nil)
		in["version"] = 2
		in["active"] = true
		f.request(t, "PUT", "/api/v1/staff/users/"+secondID, at, in, 200, nil)
		r := f.login(t, "second@spartial.test")
		in["version"] = 3
		in["role"] = "nurse"
		f.request(t, "PUT", "/api/v1/staff/users/"+secondID, at, in, 200, nil)
		f.request(t, "GET", "/api/v1/auth/me", token(r), nil, 401, nil)
	})
	t.Run("last administrator protected", func(t *testing.T) {
		uid := admin.body["user"].(map[string]any)["id"].(string)
		in := map[string]any{"display_name": "Admin", "role": "administrator", "active": false, "facility_ids": []any{f.facility}, "version": 1}
		f.request(t, "PUT", "/api/v1/staff/users/"+uid, at, in, 409, nil)
	})
	t.Run("browser cookie origin and mobile separation", func(t *testing.T) {
		in := map[string]any{"email": "admin@spartial.test", "password": password, "client_type": "web"}
		f.request(t, "POST", "/api/v1/auth/login", "", in, 403, nil)
		r := f.request(t, "POST", "/api/v1/auth/login", "", in, 200, map[string]string{"Origin": "http://localhost:3000"})
		if _, exists := r.body["refresh_token"]; exists {
			t.Fatal("web response leaked refresh token")
		}
		if !strings.Contains(r.cookie, "HttpOnly") || !strings.Contains(r.cookie, "SameSite=Strict") {
			t.Fatal("cookie flags missing")
		}
		cookie := strings.Split(r.cookie, ";")[0]
		f.request(t, "POST", "/api/v1/auth/refresh", "", map[string]any{"client_type": "web"}, 403, map[string]string{"Cookie": cookie, "Origin": "http://evil.test"})
		fresh := f.request(t, "POST", "/api/v1/auth/refresh", "", map[string]any{"client_type": "web"}, 200, map[string]string{"Cookie": cookie, "Origin": "http://localhost:3000"})
		f.request(t, "POST", "/api/v1/auth/logout", token(fresh), nil, 403, nil)
		f.request(t, "POST", "/api/v1/auth/logout", token(fresh), nil, 204, map[string]string{"Origin": "http://localhost:3000"})
		f.request(t, "GET", "/api/v1/auth/me", token(fresh), nil, 401, nil)
		in["client_type"] = "mobile"
		f.request(t, "POST", "/api/v1/auth/login", "", in, 403, map[string]string{"Origin": "http://localhost:3000"})
	})
	t.Run("durable jobs lease fencing retry and idempotent sink", func(t *testing.T) {
		j, e := platform.ClaimJob(ctx, f.runtime)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.owner.Exec(ctx, `UPDATE outbox SET lease_until=now()-interval '1 second' WHERE id=$1::uuid`, j.ID); e != nil {
			t.Fatal(e)
		}
		if e = platform.CompleteDevelopmentJob(ctx, f.runtime, j); e == nil {
			t.Fatal("expired lease completed")
		}
		// Ensure the expired job is selected first on reclaim.
		if _, e = f.owner.Exec(ctx, `UPDATE outbox SET created_at=now()-interval '1 day' WHERE id=$1::uuid`, j.ID); e != nil {
			t.Fatal(e)
		}
		next, e := platform.ClaimJob(ctx, f.runtime)
		if e != nil {
			t.Fatal(e)
		}
		if next.ID != j.ID || next.Lease == j.Lease {
			t.Fatal("lease was not reclaimed")
		}
		if e = platform.CompleteDevelopmentJob(ctx, f.runtime, j); e == nil {
			t.Fatal("old worker crossed lease fence")
		}
		if e = platform.CompleteDevelopmentJob(ctx, f.runtime, next); e != nil {
			t.Fatal(e)
		}
		if e = platform.CompleteDevelopmentJob(ctx, f.runtime, next); e == nil {
			t.Fatal("duplicate completion accepted")
		}
		var n int
		if e = f.owner.QueryRow(ctx, `SELECT count(*) FROM development_event_sink WHERE event_id=$1::uuid`, j.ID).Scan(&n); e != nil || n != 1 {
			t.Fatalf("sink count %d: %v", n, e)
		}
		other, e := platform.ClaimJob(ctx, f.runtime)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.owner.Exec(ctx, `UPDATE outbox SET lease_until=now()-interval '1 second',attempts=5 WHERE id=$1::uuid`, other.ID); e != nil {
			t.Fatal(e)
		}
		if e = platform.ReapExhaustedJobs(ctx, f.runtime); e != nil {
			t.Fatal(e)
		}
		var status string
		if e = f.owner.QueryRow(ctx, `SELECT status FROM outbox WHERE id=$1::uuid`, other.ID).Scan(&status); e != nil || status != "failed" {
			t.Fatalf("exhausted job state %q: %v", status, e)
		}
	})
	t.Run("password change logout and session ownership", func(t *testing.T) {
		// Reset only the test rate buckets to keep this lifecycle check independent of earlier logins.
		if _, e := f.owner.Exec(ctx, `TRUNCATE auth_limits`); e != nil {
			t.Fatal(e)
		}
		r := f.login(t, "reception@spartial.test")
		sessionList := f.request(t, "GET", "/api/v1/auth/sessions", token(r), nil, 200, nil)
		sid := sessionList.body["items"].([]any)[0].(map[string]any)["id"].(string)
		f.request(t, "POST", "/api/v1/auth/sessions/"+sid+"/revoke", at, nil, 404, nil)
		f.request(t, "POST", "/api/v1/auth/password", token(r), map[string]any{"current_password": password, "new_password": "Replacement-password-345!"}, 204, nil)
		f.request(t, "GET", "/api/v1/auth/me", token(r), nil, 401, nil)
		r = f.request(t, "POST", "/api/v1/auth/login", "", map[string]any{"email": "reception@spartial.test", "password": "Replacement-password-345!", "client_type": "mobile"}, 200, nil)
		f.request(t, "POST", "/api/v1/auth/logout", token(r), nil, 204, nil)
		f.request(t, "GET", "/api/v1/auth/me", token(r), nil, 401, nil)
	})
	t.Run("concurrent refresh consumes once and reuse revokes the winner", func(t *testing.T) {
		if _, e := f.owner.Exec(ctx, `TRUNCATE auth_limits`); e != nil {
			t.Fatal(e)
		}
		initial := f.login(t, "admin@spartial.test")
		results := make(chan response, 2)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for i := 0; i < 2; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				r, e := f.call("POST", "/api/v1/auth/refresh", "", map[string]any{"client_type": "mobile", "refresh_token": initial.body["refresh_token"]}, nil)
				results <- r
				errs <- e
			}()
		}
		wg.Wait()
		close(results)
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		successes, denials := 0, 0
		var winner response
		for r := range results {
			switch r.status {
			case 200:
				successes++
				winner = r
			case 401:
				denials++
			default:
				t.Fatalf("unexpected refresh status %d: %v", r.status, r.body)
			}
		}
		if successes != 1 || denials != 1 {
			t.Fatalf("rotation results success=%d denial=%d", successes, denials)
		}
		f.request(t, "GET", "/api/v1/auth/me", token(winner), nil, 401, nil)
	})
	t.Run("database rate limit and credential redaction", func(t *testing.T) {
		if _, e := f.owner.Exec(ctx, `TRUNCATE auth_limits`); e != nil {
			t.Fatal(e)
		}
		repo := identity.Repository{DB: f.runtime}
		for i := 0; i < 10; i++ {
			ok, e := repo.Limit(ctx, "ip:"+security.Digest("0.0.0.0"))
			if e != nil || !ok {
				t.Fatalf("limit iteration %d: %v", i, e)
			}
		}
		// app.Test uses a direct peer of 0.0.0.0; exercise the HTTP limiter too.
		ok, e := repo.Limit(ctx, "ip:"+security.Digest("0.0.0.0"))
		if e != nil || ok {
			t.Fatal("limit did not deny")
		}
		f.request(t, "POST", "/api/v1/auth/login", "", map[string]any{"email": "admin@spartial.test", "password": password, "client_type": "mobile"}, 429, nil)
		b, _ := json.Marshal(admin.body)
		if strings.Contains(string(b), "password_hash") {
			t.Fatal("credential hash leaked")
		}
	})
}
func TestConcurrentAdministratorProtection(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	repo := identity.Repository{DB: f.runtime}
	admin := f.login(t, "admin@spartial.test")
	a, e := repo.Actor(ctx, token(admin))
	if e != nil {
		t.Fatal(e)
	}
	service := identity.Service{Repo: repo}
	second, e := service.CreateStaff(ctx, a, identity.StaffInput{Email: "admin2@spartial.test", DisplayName: "Admin2", Password: password, Role: "administrator", FacilityIDs: []string{f.facility}}, uuid.NewString())
	if e != nil {
		t.Fatal(e)
	}
	bLogin := f.login(t, "admin2@spartial.test")
	b, e := repo.Actor(ctx, token(bLogin))
	if e != nil {
		t.Fatal(e)
	}
	active := false
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, current := range []identity.Actor{a, b} {
		wg.Add(1)
		go func(current identity.Actor) {
			defer wg.Done()
			_, e := service.UpdateStaff(ctx, current, current.ID, identity.UpdateStaff{DisplayName: current.DisplayName, Role: "administrator", Active: &active, FacilityIDs: []string{f.facility}, Version: 1}, uuid.NewString())
			errs <- e
		}(current)
	}
	wg.Wait()
	close(errs)
	successes := 0
	for e := range errs {
		if e == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("expected one administrative removal, got %d (%s)", successes, second.ID)
	}
	var n int
	if e = f.owner.QueryRow(ctx, `SELECT count(*) FROM staff WHERE role_name='administrator' AND active`).Scan(&n); e != nil || n != 1 {
		t.Fatal(fmt.Sprintf("remaining admins %d, error %v", n, e))
	}
}
