package reports

import (
	"context"

	"github.com/jackc/pgx/v5"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
)

type Raw = platform.Raw

func (s Service) DailyVisits(ctx context.Context, a identity.Actor, from, to, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "report.view"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT (to_jsonb(v) - 'org_id') FROM reporting.v_daily_visits v
 WHERE ($1='' OR v.visit_date>=$1::date) AND ($2='' OR v.visit_date<=$2::date)
 ORDER BY v.visit_date DESC, v.encounter_type LIMIT 200`, from, to)
	})
}

func (s Service) DailyRevenue(ctx context.Context, a identity.Actor, from, to, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "report.view"); e != nil {
		return nil, e
	}
	if e := platform.Need(a.Permissions, "billing.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT (to_jsonb(v) - 'org_id') FROM reporting.v_daily_revenue v
 WHERE ($1='' OR v.payment_date>=$1::date) AND ($2='' OR v.payment_date<=$2::date)
 ORDER BY v.payment_date DESC, v.method LIMIT 200`, from, to)
	})
}

func (s Service) Receivables(ctx context.Context, a identity.Actor, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "report.view"); e != nil {
		return nil, e
	}
	if e := platform.Need(a.Permissions, "billing.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT (to_jsonb(v) - 'org_id') FROM reporting.v_receivables v ORDER BY v.outstanding DESC LIMIT 200`)
	})
}

func (s Service) BillingReconciliation(ctx context.Context, a identity.Actor, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "report.view"); e != nil {
		return nil, e
	}
	if e := platform.Need(a.Permissions, "billing.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT (to_jsonb(v) - 'org_id') FROM reporting.v_billing_reconciliation_exceptions v LIMIT 200`)
	})
}

func (s Service) QueueWaitTimes(ctx context.Context, a identity.Actor, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "report.view"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT (to_jsonb(v) - 'org_id') FROM reporting.v_queue_wait_times v ORDER BY v.queue_date DESC LIMIT 200`)
	})
}

func (s Service) StockExpiry(ctx context.Context, a identity.Actor, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "report.view"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT (to_jsonb(v) - 'org_id') FROM reporting.v_stock_expiry v ORDER BY v.expiry_date LIMIT 200`)
	})
}

func (s Service) DiagnosisCounts(ctx context.Context, a identity.Actor, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "clinical.read"); e != nil {
		return nil, e
	}
	if e := platform.Need(a.Permissions, "consultation.write", "report.view"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT (to_jsonb(v) - 'org_id') FROM reporting.v_diagnosis_counts v ORDER BY v.month DESC, v.cases DESC LIMIT 200`)
	})
}

func (s Service) BreakGlassPending(ctx context.Context, a identity.Actor, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "audit.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT (to_jsonb(v) - 'org_id') FROM reporting.v_break_glass_pending_review v ORDER BY v.started_at DESC LIMIT 200`)
	})
}
