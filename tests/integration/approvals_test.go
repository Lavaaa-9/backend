package integration

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestIndependentAdministratorApprovals(t *testing.T) {
	f, _ := mfaFixture(t)
	ctx := context.Background()
	first, _, _ := enrollMFA(t, f, "admin@spartial.test")
	second := f.request(t, "POST", "/api/v1/staff/users", first, map[string]any{"email": "second-admin@spartial.test", "display_name": "Synthetic second administrator", "password": password, "role": "reception", "facility_ids": []string{f.facility}}, 201, nil)
	// Bootstrap only inside the isolated test DB; ordinary APIs must not bypass dual control.
	if _, e := f.owner.Exec(ctx, `UPDATE staff SET role_name='administrator' WHERE id=$1::uuid`, second.body["id"]); e != nil {
		t.Fatal(e)
	}
	other, _, _ := enrollMFA(t, f, "second-admin@spartial.test")
	target := f.request(t, "POST", "/api/v1/staff/users", first, map[string]any{"email": "candidate@spartial.test", "display_name": "Synthetic candidate", "password": password, "role": "reception", "facility_ids": []string{f.facility}}, 201, nil)
	candidate := token(f.login(t, "candidate@spartial.test"))
	id := target.body["id"].(string)
	f.request(t, "POST", "/api/v1/staff/users", first, map[string]any{"email": "forbidden-admin@spartial.test", "display_name": "Single admin grant denied", "password": password, "role": "administrator", "facility_ids": []string{f.facility}}, 409, nil)
	f.request(t, "PUT", "/api/v1/staff/users/"+id, first, map[string]any{"display_name": "Candidate", "role": "administrator", "active": true, "facility_ids": []string{f.facility}, "version": 1}, 409, nil)
	request := f.request(t, "POST", "/api/v1/staff/approvals", first, map[string]any{"kind": "admin_role_grant", "subject_id": id, "reason": "Clinic-approved synthetic administrator role change"}, 201, map[string]string{"Idempotency-Key": "grant-admin-approval"})
	aid := request.body["id"].(string)
	note := map[string]any{"note": "Independently verified synthetic authorization evidence"}
	f.request(t, "POST", "/api/v1/staff/approvals/"+aid+"/approve", first, note, 403, nil)
	// Competing approvals execute the role change once; repeats return the terminal result.
	results := make(chan int, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, e := f.call("POST", "/api/v1/staff/approvals/"+aid+"/approve", other, note, nil)
			results <- r.status
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
	for status := range results {
		if status != 200 {
			t.Fatalf("approval status %d", status)
		}
	}
	f.request(t, "GET", "/api/v1/auth/me", candidate, nil, 401, nil)
	var version int
	var role string
	if e := f.owner.QueryRow(ctx, `SELECT version,role_name FROM staff WHERE id=$1::uuid`, id).Scan(&version, &role); e != nil || version != 2 || role != "administrator" {
		t.Fatal("role grant executed incorrectly or repeatedly")
	}
	// Request a reset of the first admin, approved by the independently authenticated second admin.
	me := f.request(t, "GET", "/api/v1/auth/me", first, nil, 200, nil)
	reset := f.request(t, "POST", "/api/v1/staff/approvals", first, map[string]any{"kind": "mfa_reset", "subject_id": me.body["id"], "reason": "Synthetic identity verification and administrator recovery request"}, 201, map[string]string{"Idempotency-Key": "admin-mfa-reset-v1"})
	f.request(t, "POST", "/api/v1/staff/approvals/"+reset.body["id"].(string)+"/approve", other, note, 200, nil)
	f.request(t, "GET", "/api/v1/auth/me", first, nil, 401, nil)
	var n int
	if e := f.owner.QueryRow(ctx, `SELECT count(*) FROM mfa_recovery_codes WHERE code_hash IN (SELECT code_hash FROM mfa_recovery_codes WHERE staff_id=$1::uuid)`, me.body["id"]).Scan(&n); e != nil || n != 0 {
		t.Fatal("reset retained recovery codes")
	}
	if _, e := f.owner.Exec(ctx, `DELETE FROM auth_limits`); e != nil {
		t.Fatal(e)
	}
	// The reset account logs in with password but cannot act as an admin until re-enrolled.
	passwordOnly := token(f.login(t, "admin@spartial.test"))
	f.request(t, "GET", "/api/v1/staff/users", passwordOnly, nil, 403, nil)
	// Expired requests cannot execute later, even with a legitimate approver.
	pending := f.request(t, "POST", "/api/v1/staff/approvals", other, map[string]any{"kind": "mfa_reset", "subject_id": second.body["id"], "reason": "Synthetic expired request must never execute actions"}, 201, map[string]string{"Idempotency-Key": "expired-reset-test"})
	if _, e := f.owner.Exec(ctx, `UPDATE approval_requests SET created_at=now()-interval '2 hours',expires_at=now()-interval '1 hour' WHERE id=$1::uuid`, pending.body["id"]); e != nil {
		t.Fatal(e)
	}
	// Use the newly promoted account after legitimate MFA enrollment as the distinct reviewer.
	promoted, _, _ := enrollMFA(t, f, "candidate@spartial.test")
	f.request(t, "POST", "/api/v1/staff/approvals/"+pending.body["id"].(string)+"/approve", promoted, note, 409, nil)
}
func TestWebIdleExpiryAndDeviceLabel(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	r := f.request(t, "POST", "/api/v1/auth/login", "", map[string]any{"email": "admin@spartial.test", "password": password, "client_type": "web", "device_label": "Synthetic reception workstation"}, 200, map[string]string{"Origin": "http://localhost:3000"})
	sessions := f.request(t, "GET", "/api/v1/auth/sessions", token(r), nil, 200, nil)
	item := sessions.body["items"].([]any)[0].(map[string]any)
	if item["device_label"] != "Synthetic reception workstation" {
		t.Fatal("device label was lost")
	}
	if _, e := f.owner.Exec(ctx, `UPDATE sessions SET last_seen_at=now()-interval '16 minutes' WHERE id=$1::uuid`, item["id"]); e != nil {
		t.Fatal(e)
	}
	f.request(t, "GET", "/api/v1/auth/me", token(r), nil, 401, nil)
	f.request(t, "POST", "/api/v1/auth/refresh", "", map[string]any{"client_type": "web"}, 401, map[string]string{"Origin": "http://localhost:3000", "Cookie": strings.Split(r.cookie, ";")[0]})
	mobile := f.login(t, "reception@spartial.test")
	if _, e := f.owner.Exec(ctx, `UPDATE sessions SET last_seen_at=$1 WHERE staff_id=(SELECT id FROM staff WHERE email='reception@spartial.test')`, time.Now().Add(-time.Hour)); e != nil {
		t.Fatal(e)
	}
	f.request(t, "GET", "/api/v1/auth/me", token(mobile), nil, 200, nil)
}
