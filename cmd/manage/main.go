// manage runs database operations for the Spartial HMS schema (see db/migrations).
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/config"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
)

func main() {
	if e := entry(); e != nil {
		slog.Error("management_command_failed", "reason", e.Error())
		os.Exit(1)
	}
}
func entry() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: manage migrate|seed|bootstrap-development|new-encryption-key|rotate-keys")
	}
	switch os.Args[1] {
	case "new-encryption-key":
		k, e := security.NewKeyMaterial()
		if e != nil {
			return e
		}
		fmt.Println(k)
		return nil
	case "rotate-keys":
		return rotateKeys()
	case "migrate":
		return migrate()
	case "seed":
		return seed()
	case "bootstrap-development":
		if e := migrate(); e != nil {
			return e
		}
		return seed()
	}
	return fmt.Errorf("unknown management command")
}

func adminURL() (string, error) {
	cfg, e := config.Load()
	if e != nil {
		return "", e
	}
	if cfg.Env != "development" {
		return "", fmt.Errorf("management commands that create data are allowed only with APP_ENV=development")
	}
	u := os.Getenv("DATABASE_ADMIN_URL")
	if u == "" {
		return "", fmt.Errorf("DATABASE_ADMIN_URL is required")
	}
	return u, nil
}

// migrate applies db/migrations with its checksummed runner.
func migrate() error {
	raw, e := adminURL()
	if e != nil {
		return e
	}
	u, e := url.Parse(raw)
	if e != nil {
		return fmt.Errorf("invalid DATABASE_ADMIN_URL")
	}
	pw, _ := u.User.Password()
	script, e := filepath.Abs("db/tools/migrate.sh")
	if e != nil {
		return e
	}
	cmd := exec.Command(script, strings.TrimPrefix(u.Path, "/"))
	cmd.Env = append(os.Environ(), "PGHOST="+u.Hostname(), "PGPORT="+u.Port(), "PGUSER="+u.User.Username(), "PGPASSWORD="+pw)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

type seedUser struct{ email, name, role, pwEnv string }

var seedUsers = []seedUser{
	{"admin@spartial.test", "Synthetic Administrator", "administrator", "SEED_ADMIN_PASSWORD"},
	{"reception@spartial.test", "Synthetic Receptionist", "receptionist", "SEED_RECEPTION_PASSWORD"},
	{"nurse@spartial.test", "Synthetic Nurse", "nurse", "SEED_TEST_PASSWORD"},
	{"doctor@spartial.test", "Synthetic Doctor", "doctor", "SEED_TEST_PASSWORD"},
	{"laboratory@spartial.test", "Synthetic Lab Technician", "lab_technician", "SEED_TEST_PASSWORD"},
	{"lab-supervisor@spartial.test", "Synthetic Lab Supervisor", "lab_supervisor", "SEED_TEST_PASSWORD"},
	{"pharmacy@spartial.test", "Synthetic Pharmacist", "pharmacist", "SEED_TEST_PASSWORD"},
	{"cashier@spartial.test", "Synthetic Cashier", "cashier", "SEED_TEST_PASSWORD"},
	{"billing-supervisor@spartial.test", "Synthetic Billing Supervisor", "billing_supervisor", "SEED_TEST_PASSWORD"},
	{"management@spartial.test", "Synthetic Manager", "manager", "SEED_TEST_PASSWORD"},
	{"privacy-reviewer@spartial.test", "Synthetic Compliance Officer", "compliance_officer", "SEED_TEST_PASSWORD"},
}

// seed creates a synthetic organization, facility and one account per role. Existing accounts are never reset.
func seed() error {
	raw, e := adminURL()
	if e != nil {
		return e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	db, e := pgxpool.New(ctx, raw)
	if e != nil {
		return fmt.Errorf("database connection failed")
	}
	defer db.Close()
	tx, e := db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var org, facility string
	e = tx.QueryRow(ctx, `SELECT id::text FROM org.organization WHERE code='SPARTIAL'`).Scan(&org)
	if errors.Is(e, pgx.ErrNoRows) {
		e = tx.QueryRow(ctx, `INSERT INTO org.organization(code,legal_name,display_name) VALUES('SPARTIAL','Spartial Specialized Polyclinic (synthetic development)','Spartial Polyclinic') RETURNING id::text`).Scan(&org)
	}
	if e != nil {
		return e
	}
	e = tx.QueryRow(ctx, `SELECT id::text FROM org.facility WHERE org_id=$1::uuid AND code='MAIN'`, org).Scan(&facility)
	if errors.Is(e, pgx.ErrNoRows) {
		e = tx.QueryRow(ctx, `INSERT INTO org.facility(org_id,code,name) VALUES($1::uuid,'MAIN','Synthetic Development Clinic') RETURNING id::text`, org).Scan(&facility)
	}
	if e != nil {
		return e
	}
	for _, c := range [][2]string{{"WOUND-DRESS", "Wound dressing"}, {"SUTURE", "Suturing"}, {"INJECTION", "Injection administration"}} {
		if _, e = tx.Exec(ctx, `INSERT INTO billing.service_catalog(org_id,code,name,category) VALUES($1::uuid,$2,$3,'procedure') ON CONFLICT (org_id,code) DO NOTHING`, org, c[0], c[1]); e != nil {
			return e
		}
	}
	if e = seedReference(ctx, tx, org, facility); e != nil {
		return e
	}
	created := 0
	for _, u := range seedUsers {
		var exists bool
		if e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM iam.user_account WHERE account_type='staff' AND email=$1)`, u.email).Scan(&exists); e != nil {
			return e
		}
		if exists {
			continue
		}
		pw := os.Getenv(u.pwEnv)
		if !security.PasswordAllowed(pw) {
			return fmt.Errorf("%s must contain 12–256 characters and not be a common password", u.pwEnv)
		}
		hash, e := security.HashPassword(pw)
		if e != nil {
			return e
		}
		var uid, mid string
		if e = tx.QueryRow(ctx, `INSERT INTO iam.user_account(account_type,email,display_name,status,mfa_required,is_test_account) VALUES('staff',$1,$2,'active',false,true) RETURNING id::text`, u.email, u.name).Scan(&uid); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO iam.password_credential(user_id,password_hash) VALUES($1::uuid,$2)`, uid, hash); e != nil {
			return e
		}
		if e = tx.QueryRow(ctx, `INSERT INTO iam.membership(org_id,user_id,status) VALUES($1::uuid,$2::uuid,'active') RETURNING id::text`, org, uid).Scan(&mid); e != nil {
			return e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO iam.membership_role(org_id,membership_id,role_id,facility_id) SELECT $1::uuid,$2::uuid,r.id,$4::uuid FROM iam.role r WHERE r.org_id IS NULL AND r.code=$3`, org, mid, u.role, facility); e != nil {
			return e
		}
		created++
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	_ = platform.LatestMigration
	fmt.Printf("Synthetic organization SPARTIAL ready (facility %s); %d new accounts, %d total roles seeded\n", facility, created, len(seedUsers))
	return nil
}

// seedReference adds the minimum catalogue a clinic needs to try every workflow: departments, a self-pay
// payer, a few drugs and tests with their billable services and default prices, and procedure prices.
func seedReference(ctx context.Context, tx pgx.Tx, org, facility string) error {
	run := func(q string, args ...any) error { _, e := tx.Exec(ctx, q, args...); return e }
	for _, d := range [][3]string{{"RECEPTION", "Reception", "reception"}, {"TRIAGE", "Triage", "triage"}, {"OPD", "Outpatient Consultation", "consultation"},
		{"LAB", "Laboratory", "laboratory"}, {"PHARMACY", "Pharmacy", "pharmacy"}, {"CASHIER", "Cashier", "billing"}} {
		if e := run(`INSERT INTO org.department(org_id,facility_id,code,name,dept_type) VALUES($1::uuid,$2::uuid,$3,$4,$5) ON CONFLICT DO NOTHING`, org, facility, d[0], d[1], d[2]); e != nil {
			return e
		}
	}
	if e := run(`INSERT INTO billing.payer(org_id,code,name,payer_type) VALUES($1::uuid,'SELF','Self pay','self_pay') ON CONFLICT (org_id,code) DO NOTHING`, org); e != nil {
		return e
	}
	for _, d := range [][5]string{{"PARA500", "Paracetamol", "tablet", "500 mg", "tablet"}, {"AMOX500", "Amoxicillin", "capsule", "500 mg", "capsule"}, {"ORS", "Oral rehydration salts", "powder", "20.5 g", "sachet"}} {
		if e := run(`INSERT INTO pharmacy.drug(org_id,code,generic_name,dosage_form,strength,unit_of_issue) VALUES($1::uuid,$2,$3,$4,$5,$6) ON CONFLICT (org_id,code) DO NOTHING`, org, d[0], d[1], d[2], d[3], d[4]); e != nil {
			return e
		}
	}
	if e := run(`INSERT INTO diag.test_catalog(org_id,code,name,category,specimen_type,turnaround_hours) VALUES($1::uuid,'MRDT','Malaria rapid test','laboratory','blood',1),($1::uuid,'FBC','Full blood count','laboratory','blood',4) ON CONFLICT (org_id,code) DO NOTHING`, org); e != nil {
		return e
	}
	if e := run(`INSERT INTO diag.test_component(org_id,test_id,code,name,value_type,unit,decimal_places,allowed_values,display_order)
 SELECT $1::uuid,t.id,'MRDT','Malaria parasites','coded',NULL,NULL,ARRAY['Positive','Negative'],1 FROM diag.test_catalog t WHERE t.org_id=$1::uuid AND t.code='MRDT' ON CONFLICT (test_id,code) DO NOTHING`, org); e != nil {
		return e
	}
	if e := run(`INSERT INTO diag.test_component(org_id,test_id,code,name,value_type,unit,decimal_places,display_order)
 SELECT $1::uuid,t.id,c.code,c.name,'numeric',c.unit,1,c.ord FROM diag.test_catalog t, (VALUES ('HGB','Haemoglobin','g/dL',1),('WBC','White cell count','10^9/L',2),('PLT','Platelets','10^9/L',3)) AS c(code,name,unit,ord)
 WHERE t.org_id=$1::uuid AND t.code='FBC' ON CONFLICT (test_id,code) DO NOTHING`, org); e != nil {
		return e
	}
	if e := run(`INSERT INTO diag.reference_range(org_id,component_id,sex,low,high,critical_low,critical_high)
 SELECT $1::uuid,c.id,v.sex,v.lo,v.hi,v.clo,v.chi FROM diag.test_component c JOIN diag.test_catalog t ON t.id=c.test_id AND t.code='FBC' AND t.org_id=$1::uuid,
  (VALUES ('HGB','female',12.0,15.5,7.0,20.0),('HGB','male',13.5,17.5,7.0,20.0),('WBC',NULL,4.0,11.0,2.0,30.0),('PLT',NULL,150,450,50,1000)) AS v(code,sex,lo,hi,clo,chi)
 WHERE c.code=v.code AND NOT EXISTS (SELECT 1 FROM diag.reference_range r WHERE r.component_id=c.id)`, org); e != nil {
		return e
	}
	// billable services for drugs and tests, then default prices (TZS)
	if e := run(`INSERT INTO billing.service_catalog(org_id,code,name,category,drug_id) SELECT $1::uuid,'RX-'||d.code,d.generic_name||' '||d.strength,'pharmacy',d.id FROM pharmacy.drug d WHERE d.org_id=$1::uuid
 AND NOT EXISTS (SELECT 1 FROM billing.service_catalog s WHERE s.drug_id=d.id AND s.status='active') ON CONFLICT (org_id,code) DO NOTHING`, org); e != nil {
		return e
	}
	if e := run(`INSERT INTO billing.service_catalog(org_id,code,name,category,diag_test_id) SELECT $1::uuid,'LAB-'||t.code,t.name,'laboratory',t.id FROM diag.test_catalog t WHERE t.org_id=$1::uuid
 AND NOT EXISTS (SELECT 1 FROM billing.service_catalog s WHERE s.diag_test_id=t.id AND s.status='active') ON CONFLICT (org_id,code) DO NOTHING`, org); e != nil {
		return e
	}
	if e := run(`INSERT INTO billing.service_catalog(org_id,code,name,category) VALUES($1::uuid,'CONSULT','Outpatient consultation','consultation'),($1::uuid,'REGISTER','Registration','registration') ON CONFLICT (org_id,code) DO NOTHING`, org); e != nil {
		return e
	}
	return run(`INSERT INTO billing.price(org_id,service_id,unit_price,effective)
 SELECT $1::uuid,s.id,CASE s.code WHEN 'CONSULT' THEN 10000 WHEN 'REGISTER' THEN 2000 WHEN 'LAB-MRDT' THEN 5000 WHEN 'LAB-FBC' THEN 8000 WHEN 'RX-PARA500' THEN 100 WHEN 'RX-AMOX500' THEN 300
   WHEN 'RX-ORS' THEN 500 WHEN 'WOUND-DRESS' THEN 15000 WHEN 'SUTURE' THEN 25000 WHEN 'INJECTION' THEN 3000 END,tstzrange('2026-01-01'::timestamptz,NULL,'[)')
 FROM billing.service_catalog s WHERE s.org_id=$1::uuid AND s.code IN ('CONSULT','REGISTER','LAB-MRDT','LAB-FBC','RX-PARA500','RX-AMOX500','RX-ORS','WOUND-DRESS','SUTURE','INJECTION')
 AND NOT EXISTS (SELECT 1 FROM billing.price p WHERE p.service_id=s.id)`, org)
}
