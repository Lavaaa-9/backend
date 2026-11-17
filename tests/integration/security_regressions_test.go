package integration

import (
	"context"
	"encoding/base32"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"spartial.local/backend/internal/config"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
)

func mfaFixture(t *testing.T) (*fixture, *security.Keyring) {
	t.Helper()
	material, e := security.NewKeyMaterial()
	if e != nil {
		t.Fatal(e)
	}
	keys, e := security.ParseKeyring("test1:"+material, "test1")
	if e != nil {
		t.Fatal(e)
	}
	return setupWith(t, config.Config{Env: "test", Origins: []string{"http://localhost:3000"}, Keys: keys, MFAEnforcePrivileged: true}), keys
}
func enrollMFA(t *testing.T, f *fixture, email string) (string, []byte, []string) {
	t.Helper()
	session := token(f.login(t, email))
	r := f.request(t, "POST", "/api/v1/auth/mfa/enroll", session, map[string]any{"current_password": password}, 200, nil)
	secret, e := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(r.body["secret"].(string))
	if e != nil {
		t.Fatal("invalid MFA secret encoding")
	}
	code := security.HOTP(secret, security.TOTPStep(time.Now())-1, security.TOTPDigits)
	r = f.request(t, "POST", "/api/v1/auth/mfa/confirm", session, map[string]any{"code": code}, 200, nil)
	codes := []string{}
	for _, c := range r.body["recovery_codes"].([]any) {
		codes = append(codes, c.(string))
	}
	return session, secret, codes
}
func loginChallenge(t *testing.T, f *fixture, client string) string {
	t.Helper()
	headers := map[string]string{}
	if client == "web" {
		headers["Origin"] = "http://localhost:3000"
	}
	r := f.request(t, "POST", "/api/v1/auth/login", "", map[string]any{"email": "admin@spartial.test", "password": password, "client_type": client}, 200, headers)
	if r.body["mfa_required"] != true || r.body["access_token"] != nil || r.cookie != "" {
		t.Fatal("challenge issued credentials or a cookie")
	}
	return r.body["challenge_token"].(string)
}
func verifyRecovery(t *testing.T, f *fixture, ch, code string, status int) response {
	t.Helper()
	return f.request(t, "POST", "/api/v1/auth/mfa/verify", "", map[string]any{"challenge_token": ch, "client_type": "mobile", "recovery_code": code}, status, nil)
}
func TestRecoveryRegenerationAndConcurrentConsumption(t *testing.T) {
	f, _ := mfaFixture(t)
	ctx := context.Background()
	current, _, codes := enrollMFA(t, f, "admin@spartial.test")
	other := token(verifyRecovery(t, f, loginChallenge(t, f, "mobile"), codes[0], 200))
	pending := loginChallenge(t, f, "mobile")
	body := map[string]any{"current_password": "incorrect-password", "recovery_code": codes[1]}
	f.request(t, "POST", "/api/v1/auth/mfa/recovery-codes/regenerate", current, body, 401, nil)
	body["current_password"] = password
	regenerated := f.request(t, "POST", "/api/v1/auth/mfa/recovery-codes/regenerate", current, body, 200, nil)
	replacement := []string{}
	for _, c := range regenerated.body["recovery_codes"].([]any) {
		replacement = append(replacement, c.(string))
	}
	if len(replacement) != 10 {
		t.Fatal("incorrect recovery set size")
	}
	for _, old := range codes {
		var n int
		if e := f.owner.QueryRow(ctx, `SELECT count(*) FROM mfa_recovery_codes WHERE code_hash=$1`, security.Digest(security.NormalizeRecoveryCode(old))).Scan(&n); e != nil || n != 0 {
			t.Fatal("old recovery code survived regeneration")
		}
	}
	f.request(t, "GET", "/api/v1/auth/me", other, nil, 401, nil)
	verifyRecovery(t, f, pending, replacement[0], 401) // Account version changed; must not consume new code.
	ch := loginChallenge(t, f, "mobile")
	verifyRecovery(t, f, ch, codes[2], 401)
	signedIn := verifyRecovery(t, f, ch, replacement[0], 200)
	f.request(t, "GET", "/api/v1/staff/users", token(signedIn), nil, 200, nil)
	// Two independent challenges may not spend the same recovery code twice.
	if _, e := f.owner.Exec(ctx, `DELETE FROM auth_limits`); e != nil {
		t.Fatal(e)
	}
	challenges := []string{loginChallenge(t, f, "mobile"), loginChallenge(t, f, "mobile")}
	results := make(chan int, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for _, c := range challenges {
		wg.Add(1)
		go func(c string) {
			defer wg.Done()
			r, e := f.call("POST", "/api/v1/auth/mfa/verify", "", map[string]any{"challenge_token": c, "client_type": "mobile", "recovery_code": replacement[1]}, nil)
			results <- r.status
			errs <- e
		}(c)
	}
	wg.Wait()
	close(results)
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	accepted, denied := 0, 0
	for status := range results {
		switch status {
		case 200:
			accepted++
		case 401:
			denied++
		default:
			t.Fatalf("unexpected concurrent status %d", status)
		}
	}
	if accepted != 1 || denied != 1 {
		t.Fatalf("recovery consumption accepted=%d denied=%d", accepted, denied)
	}
	var n int
	if e := f.owner.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='auth.mfa_recovery_regenerate' AND outcome='success' AND reason='recovery_code'`).Scan(&n); e != nil || n != 1 {
		t.Fatal("recovery regeneration was not audited")
	}
}
func TestMFAEnrollmentLimitsExpiryAndRevokedSession(t *testing.T) {
	f, keys := mfaFixture(t)
	ctx := context.Background()
	session := token(f.login(t, "admin@spartial.test"))
	enrolled := f.request(t, "POST", "/api/v1/auth/mfa/enroll", session, map[string]any{"current_password": password}, 200, nil)
	secret, e := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(enrolled.body["secret"].(string))
	if e != nil {
		t.Fatal(e)
	}
	step := security.TOTPStep(time.Now())
	bad := ""
	for n := 0; n < 100; n++ {
		candidate := fmt.Sprintf("%06d", n)
		valid := false
		for d := int64(-1); d <= 1; d++ {
			valid = valid || candidate == security.HOTP(secret, step+d, 6)
		}
		if !valid {
			bad = candidate
			break
		}
	}
	for i := 0; i < 9; i++ {
		f.request(t, "POST", "/api/v1/auth/mfa/confirm", session, map[string]any{"code": bad}, 422, nil)
	}
	f.request(t, "POST", "/api/v1/auth/mfa/confirm", session, map[string]any{"code": security.HOTP(secret, step, 6)}, 429, nil)
	var denied int
	if e = f.owner.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='auth.mfa_confirm' AND outcome='denied'`).Scan(&denied); e != nil || denied != 9 {
		t.Fatal("MFA failures were not durably audited")
	}
	if _, e = f.owner.Exec(ctx, `DELETE FROM auth_limits`); e != nil {
		t.Fatal(e)
	}
	if _, e = f.owner.Exec(ctx, `UPDATE staff_mfa SET updated_at=now()-interval '11 minutes'`); e != nil {
		t.Fatal(e)
	}
	f.request(t, "POST", "/api/v1/auth/mfa/confirm", session, map[string]any{"code": security.HOTP(secret, step, 6)}, 409, nil)
	repo := identity.Repository{DB: f.runtime}
	a, e := repo.Actor(ctx, session)
	if e != nil {
		t.Fatal(e)
	}
	service := identity.Service{Repo: repo, Keys: keys}
	if e = service.Revoke(ctx, a, a.SessionID, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	// Simulate a request authorized before logout but waiting to acquire transaction locks.
	if _, e = service.StartEnrollment(ctx, a, password, uuid.NewString()); e == nil {
		t.Fatal("revoked session restarted MFA enrollment")
	}
	if _, e = service.ConfirmEnrollment(ctx, a, security.HOTP(secret, step, 6), uuid.NewString()); e == nil {
		t.Fatal("revoked session confirmed MFA")
	}
}
func TestMFAChallengeExpiryAndBrowserFlow(t *testing.T) {
	f, _ := mfaFixture(t)
	ctx := context.Background()
	_, _, codes := enrollMFA(t, f, "admin@spartial.test")
	ch := loginChallenge(t, f, "mobile")
	if _, e := f.owner.Exec(ctx, `UPDATE login_challenges SET expires_at=now()-interval '1 second' WHERE token_hash=$1`, security.Digest(ch)); e != nil {
		t.Fatal(e)
	}
	verifyRecovery(t, f, ch, codes[0], 401)
	var before int
	if e := f.owner.QueryRow(ctx, `SELECT count(*) FROM sessions`).Scan(&before); e != nil {
		t.Fatal(e)
	}
	web := loginChallenge(t, f, "web")
	var after int
	if e := f.owner.QueryRow(ctx, `SELECT count(*) FROM sessions`).Scan(&after); e != nil || before != after {
		t.Fatal("password challenge created a session")
	}
	body := map[string]any{"challenge_token": web, "client_type": "web", "recovery_code": codes[0]}
	f.request(t, "POST", "/api/v1/auth/mfa/verify", "", body, 403, map[string]string{"Origin": "http://evil.test"})
	wrongClient := map[string]any{"challenge_token": web, "client_type": "mobile", "recovery_code": codes[0]}
	f.request(t, "POST", "/api/v1/auth/mfa/verify", "", wrongClient, 401, nil)
	r := f.request(t, "POST", "/api/v1/auth/mfa/verify", "", body, 200, map[string]string{"Origin": "http://localhost:3000"})
	if _, ok := r.body["refresh_token"]; ok || !strings.Contains(r.cookie, "HttpOnly") {
		t.Fatal("MFA browser credentials were not cookie-only")
	}
	cookie := strings.Split(r.cookie, ";")[0]
	refreshed := f.request(t, "POST", "/api/v1/auth/refresh", "", map[string]any{"client_type": "web"}, 200, map[string]string{"Cookie": cookie, "Origin": "http://localhost:3000"})
	me := f.request(t, "GET", "/api/v1/auth/me", token(refreshed), nil, 200, nil)
	if me.body["mfa_verified"] != true {
		t.Fatal("refresh lost MFA assurance")
	}
	f.request(t, "GET", "/api/v1/staff/users", token(refreshed), nil, 200, nil)
}
func TestAuditSealingWaitsForWriters(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	tx, e := f.owner.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback(ctx)
	if e = platform.Audit(ctx, tx, "", "synthetic.pending", "", "", uuid.NewString(), "success"); e != nil {
		t.Fatal(e)
	}
	// Higher identity commits while the lower identity is still uncommitted.
	if _, e = f.owner.Exec(ctx, `INSERT INTO audit_events(action,request_id,outcome) VALUES('synthetic.committed',$1::uuid,'success')`, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	conn, e := f.owner.Acquire(ctx)
	if e != nil {
		t.Fatal(e)
	}
	pid := conn.Conn().PgConn().PID()
	type result struct {
		s platform.Seal
		e error
	}
	done := make(chan result, 1)
	go func() {
		defer conn.Release()
		var s platform.Seal
		e := conn.QueryRow(ctx, `SELECT sealed_count,last_event_id,chain_hash FROM seal_audit_events(interval '0')`).Scan(&s.Count, &s.LastEventID, &s.ChainHash)
		done <- result{s, e}
	}()
	deadline := time.Now().Add(1500 * time.Millisecond)
	for {
		select {
		case r := <-done:
			t.Fatalf("sealer passed an uncommitted writer: count=%d error=%v", r.s.Count, r.e)
		default:
		}
		var waiting bool
		if e = f.owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE pid=$1 AND mode='ShareLock' AND NOT granted)`, pid).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sealer did not wait for writer lock")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	r := <-done
	if r.e != nil {
		t.Fatal(r.e)
	}
	report, e := platform.VerifyAuditChain(ctx, f.runtime)
	if e != nil || !report.Intact() || report.Unsealed != 0 || report.Events != int64(r.s.Count) {
		t.Fatalf("out-of-order commit broke audit coverage: %+v %v", report, e)
	}
	if _, e = platform.SealAudit(ctx, f.runtime, 0); e == nil {
		t.Fatal("runtime role could create audit seals")
	}
}
func TestAuditSettleWindowNeverSkipsAPrefix(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	if _, e := f.owner.Exec(ctx, `INSERT INTO audit_events(action,request_id,outcome,created_at) VALUES('synthetic.old-later-identity',$1::uuid,'success',now()-interval '1 hour')`, uuid.NewString()); e != nil {
		t.Fatal(e)
	}
	s, e := platform.SealAudit(ctx, f.owner, 5*time.Minute)
	if e != nil || s.Count != 0 {
		t.Fatalf("age filter skipped younger lower identities: %+v %v", s, e)
	}
	report, e := platform.VerifyAuditChain(ctx, f.runtime)
	if e != nil || report.Unsealed < 3 {
		t.Fatalf("unsealed events not reported: %+v %v", report, e)
	}
	if _, e = platform.SealAudit(ctx, f.owner, 0); e != nil {
		t.Fatal(e)
	}
	report, e = platform.VerifyAuditChain(ctx, f.runtime)
	if e != nil || !report.Intact() || report.Unsealed != 0 {
		t.Fatalf("prefix failed verification: %+v %v", report, e)
	}
	if _, e = f.runtime.Exec(ctx, `INSERT INTO audit_seals(first_event_id,last_event_id,event_count,previous_hash,chain_hash) VALUES(999,999,1,decode(repeat('00',32),'hex'),decode(repeat('00',32),'hex'))`); e == nil {
		t.Fatal("runtime role could fabricate a seal")
	}
}
