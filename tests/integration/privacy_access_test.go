package integration

import (
	"context"
	"github.com/google/uuid"
	"spartial.local/backend/internal/config"
	"spartial.local/backend/internal/platform"
	"sync"
	"testing"
	"time"
)

func TestCarePolicyBreakGlassAndRLS(t *testing.T) {
	f := setupWith(t, config.Config{Env: "test", Origins: []string{"http://localhost:3000"}, CarePolicyEnabled: true})
	ctx := context.Background()
	admin := token(f.login(t, "admin@spartial.test"))
	reception := token(f.login(t, "reception@spartial.test"))
	create := func(email, role string) (string, string) {
		u := f.request(t, "POST", "/api/v1/staff/users", admin, map[string]any{"email": email, "display_name": "Synthetic Care Staff", "password": password, "role": role, "facility_ids": []string{f.facility}}, 201, nil)
		return u.body["id"].(string), token(f.login(t, email))
	}
	doctorID, doctor := create("care-doctor@spartial.test", "doctor")
	_, reviewer := create("care-review@spartial.test", "privacy-reviewer")
	patient := f.request(t, "POST", "/api/v1/staff/patients", reception, f.patientInput(), 201, map[string]string{"Idempotency-Key": "care-test-patient"})
	pid := patient.body["id"].(string)
	t.Run("RLS denies absent actor and pooled actor state never escapes", func(t *testing.T) {
		var n int
		if e := f.runtime.QueryRow(ctx, `SELECT count(*) FROM patients`).Scan(&n); e != nil || n != 0 {
			t.Fatalf("unscoped runtime rows=%d error=%v", n, e)
		}
		tx, e := platform.BeginActor(ctx, f.runtime, doctorID, true)
		if e != nil {
			t.Fatal(e)
		}
		if e = tx.QueryRow(ctx, `SELECT count(*) FROM patients`).Scan(&n); e != nil || n != 0 {
			t.Fatalf("unassigned doctor saw %d rows: %v", n, e)
		}
		if e = tx.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		if e = f.runtime.QueryRow(ctx, `SELECT count(*) FROM patients`).Scan(&n); e != nil || n != 0 {
			t.Fatal("actor setting leaked across transactions")
		}
	})
	t.Run("no relationship returns audited not-found", func(t *testing.T) {
		f.request(t, "GET", "/api/v1/staff/patients/"+pid, doctor, nil, 404, nil)
		var n int
		if e := f.owner.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE actor_id=$1::uuid AND patient_id=$2::uuid AND outcome='denied' AND reason='care_relationship_required'`, doctorID, pid).Scan(&n); e != nil || n != 1 {
			t.Fatal("snooping denial was not attributed to patient")
		}
	})
	t.Run("assignment grants and revokes demographic access", func(t *testing.T) {
		idempotency := uuid.NewString()
		in := map[string]any{"staff_id": doctorID, "reason": "Responsible clinician for synthetic care", "valid_until": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)}
		assigned := f.request(t, "POST", "/api/v1/staff/patients/"+pid+"/care-assignments", reception, in, 201, map[string]string{"Idempotency-Key": idempotency})
		replay := f.request(t, "POST", "/api/v1/staff/patients/"+pid+"/care-assignments", reception, in, 201, map[string]string{"Idempotency-Key": idempotency})
		if assigned.body["id"] != replay.body["id"] {
			t.Fatal("assignment duplicated")
		}
		f.request(t, "GET", "/api/v1/staff/patients/"+pid, doctor, nil, 200, nil)
		f.request(t, "POST", "/api/v1/staff/care-assignments/"+assigned.body["id"].(string)+"/revoke", reception, nil, 200, nil)
		f.request(t, "GET", "/api/v1/staff/patients/"+pid, doctor, nil, 404, nil)
	})
	var grant string
	t.Run("concurrent exceptional grants have one business effect", func(t *testing.T) {
		body := map[string]any{"reason": "Urgent synthetic treatment while allocation is unavailable", "duration_minutes": 240}
		results := make(chan response, 2)
		errs := make(chan error, 2)
		var wg sync.WaitGroup
		for _, key := range []string{"emergency-key-one", "emergency-key-two"} {
			wg.Add(1)
			go func(key string) {
				defer wg.Done()
				r, e := f.call("POST", "/api/v1/staff/patients/"+pid+"/break-glass", doctor, body, map[string]string{"Idempotency-Key": key})
				results <- r
				errs <- e
			}(key)
		}
		wg.Wait()
		close(results)
		close(errs)
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		ok, conflict := 0, 0
		for r := range results {
			if r.status == 201 {
				ok++
				grant = r.body["id"].(string)
			} else if r.status == 409 {
				conflict++
			} else {
				t.Fatalf("unexpected grant status %d", r.status)
			}
		}
		if ok != 1 || conflict != 1 {
			t.Fatal("competing grants were not serialized")
		}
		f.request(t, "GET", "/api/v1/staff/patients/"+pid, doctor, nil, 200, nil)
		f.request(t, "POST", "/api/v1/staff/patients/"+uuid.NewString()+"/break-glass", doctor, body, 404, map[string]string{"Idempotency-Key": "nonexistent-patient"})
		body["duration_minutes"] = 241
		f.request(t, "POST", "/api/v1/staff/patients/"+pid+"/break-glass", doctor, body, 422, map[string]string{"Idempotency-Key": "too-long-emergency"})
	})
	t.Run("independent review rejects further emergency access", func(t *testing.T) {
		queue := f.request(t, "GET", "/api/v1/staff/privacy/break-glass", reviewer, nil, 200, nil)
		if len(queue.body["items"].([]any)) != 1 {
			t.Fatal("review task not created")
		}
		f.request(t, "POST", "/api/v1/staff/privacy/break-glass/"+grant+"/review", reviewer, map[string]any{"outcome": "inappropriate", "note": "Synthetic review determines no emergency existed"}, 200, nil)
		f.request(t, "GET", "/api/v1/staff/patients/"+pid, doctor, nil, 404, nil)
		f.request(t, "POST", "/api/v1/staff/privacy/break-glass/"+grant+"/review", reviewer, map[string]any{"outcome": "appropriate", "note": "Second review must never overwrite the first one"}, 409, nil)
	})
	t.Run("expiry and changed-role self review remain denied", func(t *testing.T) {
		next := f.request(t, "POST", "/api/v1/staff/patients/"+pid+"/break-glass", doctor, map[string]any{"reason": "Another synthetic emergency requiring independent review", "duration_minutes": 1}, 201, map[string]string{"Idempotency-Key": "expiry-grant-key"})
		id := next.body["id"].(string)
		// Keep DB interval checks valid while moving the whole synthetic grant into the past.
		if _, e := f.owner.Exec(ctx, `UPDATE break_glass_access SET granted_at=now()-interval '2 minutes',expires_at=now()-interval '1 minute' WHERE id=$1::uuid`, id); e != nil {
			t.Fatal(e)
		}
		f.request(t, "GET", "/api/v1/staff/patients/"+pid, doctor, nil, 404, nil)
		// Historic requester later becomes a privacy reviewer; identity still forbids self review.
		if _, e := f.owner.Exec(ctx, `UPDATE staff SET role_name='privacy-reviewer',version=version+1 WHERE id=$1::uuid`, doctorID); e != nil {
			t.Fatal(e)
		}
		f.request(t, "POST", "/api/v1/staff/privacy/break-glass/"+id+"/review", doctor, map[string]any{"outcome": "appropriate", "note": "I must not review my own historic exceptional access"}, 403, nil)
	})
}
