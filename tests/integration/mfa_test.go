package integration

import (
	"context"
	"encoding/base32"
	"fmt"
	"testing"
	"time"

	"spartial.local/backend/internal/config"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
)

func TestMFAAndAuditChain(t *testing.T) {
	material, e := security.NewKeyMaterial()
	if e != nil {
		t.Fatal(e)
	}
	keys, e := security.ParseKeyring("k1:"+material, "k1")
	if e != nil {
		t.Fatal(e)
	}
	f := setupWith(t, config.Config{Env: "test", Origins: []string{"http://localhost:3000"}, Keys: keys, MFAEnforcePrivileged: true})
	ctx := context.Background()
	const admin = "admin@spartial.test"
	creds := func(pw string) map[string]any {
		return map[string]any{"email": admin, "password": pw, "client_type": "mobile"}
	}
	challenge := func(t *testing.T, pw string) string {
		t.Helper()
		r := f.request(t, "POST", "/api/v1/auth/login", "", creds(pw), 200, nil)
		if r.body["mfa_required"] != true || r.body["access_token"] != nil {
			t.Fatalf("expected an MFA challenge without tokens: %v", r.body)
		}
		return r.body["challenge_token"].(string)
	}
	// Rate limits are exercised elsewhere; reset buckets so each scenario is independent.
	reset := func(t *testing.T) {
		t.Helper()
		if _, e := f.owner.Exec(ctx, `DELETE FROM auth_limits`); e != nil {
			t.Fatal(e)
		}
	}
	verify := func(t *testing.T, ch string, field, value string, status int) response {
		t.Helper()
		return f.request(t, "POST", "/api/v1/auth/mfa/verify", "", map[string]any{"challenge_token": ch, field: value, "client_type": "mobile"}, status, nil)
	}

	pwSession := token(f.login(t, admin))
	other := token(f.login(t, admin))
	reception := token(f.login(t, "reception@spartial.test"))
	var secret []byte
	baseStep := security.TOTPStep(time.Now())
	code := func(offset int64) string {
		return security.HOTP(secret, baseStep+offset, security.TOTPDigits)
	}
	var recovery []any

	t.Run("privileged role blocked until second factor; ordinary roles unaffected", func(t *testing.T) {
		r := f.request(t, "GET", "/api/v1/staff/users", pwSession, nil, 403, nil)
		if r.body["error"].(map[string]any)["code"] != "mfa_required" {
			t.Fatal(r.body)
		}
		if f.request(t, "GET", "/api/v1/auth/me", pwSession, nil, 200, nil).body["mfa_verified"] != false {
			t.Fatal("password session reported as MFA verified")
		}
		f.request(t, "GET", "/api/v1/staff/patients", reception, nil, 200, nil)
	})
	t.Run("enrollment requires password and a valid code", func(t *testing.T) {
		f.request(t, "POST", "/api/v1/auth/mfa/enroll", pwSession, map[string]any{"current_password": "wrong-password-value"}, 401, nil)
		r := f.request(t, "POST", "/api/v1/auth/mfa/enroll", pwSession, map[string]any{"current_password": password}, 200, nil)
		var err error
		secret, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(r.body["secret"].(string))
		if err != nil || len(secret) != 20 {
			t.Fatal("invalid secret encoding")
		}
		var stored []byte
		if e := f.owner.QueryRow(ctx, `SELECT secret_ciphertext FROM staff_mfa m JOIN staff s ON s.id=m.staff_id WHERE s.email=$1`, admin).Scan(&stored); e != nil {
			t.Fatal(e)
		}
		if string(stored) == string(secret) || len(stored) < 40 {
			t.Fatal("TOTP secret stored unencrypted")
		}
		wrong := "000000"
		if wrong == code(-1) || wrong == code(0) || wrong == code(1) {
			wrong = "999999"
		}
		f.request(t, "POST", "/api/v1/auth/mfa/confirm", pwSession, map[string]any{"code": wrong}, 422, nil)
		r = f.request(t, "POST", "/api/v1/auth/mfa/confirm", pwSession, map[string]any{"code": code(-1)}, 200, nil)
		recovery = r.body["recovery_codes"].([]any)
		if len(recovery) != 10 {
			t.Fatal("expected 10 recovery codes")
		}
		f.request(t, "POST", "/api/v1/auth/mfa/enroll", pwSession, map[string]any{"current_password": password}, 409, nil)
		// The confirming session is upgraded; every other session is revoked.
		f.request(t, "GET", "/api/v1/staff/users", pwSession, nil, 200, nil)
		f.request(t, "GET", "/api/v1/auth/me", other, nil, 401, nil)
	})
	t.Run("login challenge, replay and reuse protection", func(t *testing.T) {
		reset(t)
		ch := challenge(t, password)
		bad := ""
		for n := 0; n < 100; n++ {
			candidate := fmt.Sprintf("%06d", n)
			if candidate != code(-1) && candidate != code(0) && candidate != code(1) {
				bad = candidate
				break
			}
		}
		verify(t, ch, "code", bad, 401)
		verify(t, ch, "code", code(-1), 401) // step already used during confirmation
		r := verify(t, ch, "code", code(0), 200)
		f.request(t, "GET", "/api/v1/staff/users", token(r), nil, 200, nil)
		verify(t, ch, "code", code(1), 401) // consumed challenge
		ch = challenge(t, password)
		verify(t, ch, "code", code(0), 401) // same step replayed
		verify(t, ch, "code", code(1), 200)
		f.request(t, "POST", "/api/v1/auth/mfa/verify", "", map[string]any{"challenge_token": ch, "client_type": "web"}, 403, nil)
	})
	t.Run("recovery codes are single use", func(t *testing.T) {
		reset(t)
		ch := challenge(t, password)
		verify(t, ch, "recovery_code", recovery[0].(string), 200)
		verify(t, challenge(t, password), "recovery_code", recovery[0].(string), 401)
	})
	t.Run("challenge attempts are bounded", func(t *testing.T) {
		reset(t)
		ch := challenge(t, password)
		for i := 0; i < 5; i++ {
			verify(t, ch, "recovery_code", "AAAA-AAAA-AAAA-AAAA", 401)
		}
		verify(t, ch, "recovery_code", recovery[1].(string), 401)
		var used bool
		if e := f.owner.QueryRow(ctx, `SELECT used_at IS NOT NULL FROM mfa_recovery_codes WHERE code_hash=$1`, security.Digest(security.NormalizeRecoveryCode(recovery[1].(string)))).Scan(&used); e != nil || used {
			t.Fatal("exhausted challenge consumed a recovery code")
		}
	})
	newPassword := "Synthetic-rotated-password-987!"
	t.Run("password change invalidates pending challenges", func(t *testing.T) {
		reset(t)
		ch := challenge(t, password)
		s := token(verify(t, challenge(t, password), "recovery_code", recovery[2].(string), 200))
		f.request(t, "POST", "/api/v1/auth/password", s, map[string]any{"current_password": password, "new_password": newPassword}, 204, nil)
		verify(t, ch, "recovery_code", recovery[3].(string), 401)
	})
	t.Run("disable requires password and factor and revokes sessions", func(t *testing.T) {
		reset(t)
		s := token(verify(t, challenge(t, newPassword), "recovery_code", recovery[4].(string), 200))
		f.request(t, "POST", "/api/v1/auth/mfa/disable", s, map[string]any{"current_password": password, "recovery_code": recovery[5].(string)}, 401, nil)
		f.request(t, "POST", "/api/v1/auth/mfa/disable", s, map[string]any{"current_password": newPassword, "recovery_code": recovery[5].(string)}, 204, nil)
		f.request(t, "GET", "/api/v1/auth/me", s, nil, 401, nil)
		r := f.request(t, "POST", "/api/v1/auth/login", "", creds(newPassword), 200, nil)
		f.request(t, "GET", "/api/v1/staff/users", token(r), nil, 403, nil)
		var n int
		if e := f.owner.QueryRow(ctx, `SELECT count(*) FROM mfa_recovery_codes`).Scan(&n); e != nil || n != 0 {
			t.Fatal("recovery codes survived disable")
		}
	})
	t.Run("audit trail records the factor lifecycle", func(t *testing.T) {
		for _, action := range []string{"auth.mfa_enroll_start", "auth.mfa_enabled", "auth.mfa_challenge", "auth.mfa_verify", "auth.mfa_disabled"} {
			var n int
			if e := f.owner.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action=$1`, action).Scan(&n); e != nil || n == 0 {
				t.Fatalf("missing audit action %s", action)
			}
		}
		var reasons int
		if e := f.owner.QueryRow(ctx, `SELECT count(*) FROM audit_events WHERE action='access.denied' AND reason='mfa_required'`).Scan(&reasons); e != nil || reasons == 0 {
			t.Fatal("MFA denial was not audited with a reason")
		}
	})
	t.Run("audit chain seals verify and detect tampering", func(t *testing.T) {
		s, e := platform.SealAudit(ctx, f.owner, 0)
		if e != nil || s.Count == 0 || len(s.ChainHash) != 64 {
			t.Fatalf("seal failed: %+v %v", s, e)
		}
		if again, e := platform.SealAudit(ctx, f.owner, 0); e != nil || again.Count != 0 || again.ChainHash != s.ChainHash {
			t.Fatalf("empty seal changed the head: %+v %v", again, e)
		}
		r, e := platform.VerifyAuditChain(ctx, f.runtime)
		if e != nil || !r.Intact() || r.Events != int64(s.Count) {
			t.Fatalf("intact chain failed verification: %+v %v", r, e)
		}
		if _, e = f.runtime.Exec(ctx, `UPDATE audit_seals SET chain_hash=chain_hash`); e == nil {
			t.Fatal("runtime role could modify audit seals")
		}
		for _, q := range []string{`ALTER TABLE audit_events DISABLE TRIGGER audit_immutable`, `UPDATE audit_events SET outcome='success' WHERE id=(SELECT min(id) FROM audit_events WHERE outcome='denied')`, `ALTER TABLE audit_events ENABLE TRIGGER audit_immutable`} {
			if _, e = f.owner.Exec(ctx, q); e != nil {
				t.Fatal(e)
			}
		}
		if r, e = platform.VerifyAuditChain(ctx, f.runtime); e != nil || r.Intact() {
			t.Fatalf("tampering was not detected: %+v %v", r, e)
		}
	})
}

func TestMFAVerifyRateLimited(t *testing.T) {
	f := setup(t)
	limited := false
	for i := 0; i < 12; i++ {
		r, e := f.call("POST", "/api/v1/auth/mfa/verify", "", map[string]any{"challenge_token": "unknown-challenge-token-value-0000000000", "code": "123456", "client_type": "mobile"}, nil)
		if e != nil {
			t.Fatal(e)
		}
		limited = limited || r.status == 429
	}
	if !limited {
		t.Fatal("MFA verification is not rate limited")
	}
}

func TestMFAUnavailableWithoutKeys(t *testing.T) {
	f := setup(t)
	s := token(f.login(t, "admin@spartial.test"))
	r := f.request(t, "POST", "/api/v1/auth/mfa/enroll", s, map[string]any{"current_password": password}, 503, nil)
	if r.body["error"].(map[string]any)["code"] != "mfa_unavailable" {
		t.Fatal(r.body)
	}
	// Development default does not enforce MFA, so existing workflows keep working.
	f.request(t, "GET", "/api/v1/staff/users", s, nil, 200, nil)
}
