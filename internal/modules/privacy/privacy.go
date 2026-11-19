// Package privacy covers break-glass emergency access, per-patient PHI access reports,
// privacy anomaly alerts, data-subject requests, legal holds, and periodic access reviews.
package privacy

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
)

type Service struct{ DB *pgxpool.Pool }
type Raw = platform.Raw

var (
	bgJ      = `(to_jsonb(b) - 'org_id')`
	alertJ   = platform.JSON("a", true)
	reqJ     = platform.JSON("r", true)
	holdJ    = `(to_jsonb(h) - 'org_id')`
	reviewJ  = platform.JSON("r", true)
	revItemJ = `(to_jsonb(i) - 'org_id')`
)

// ---------------------------------------------------------------- break-glass

type BreakGlassInput struct {
	Reason          string `json:"reason" validate:"required,min=20,max=1000"`
	DurationMinutes int    `json:"duration_minutes" validate:"omitempty,min=5,max=240"`
}

type BreakGlassReview struct {
	Outcome string `json:"outcome" validate:"required,oneof=appropriate inappropriate needs_follow_up"`
	Note    string `json:"note" validate:"max=1000"`
}

func (s Service) OpenBreakGlass(ctx context.Context, a identity.Actor, patientID string, in BreakGlassInput, key, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "break_glass.use"); e != nil {
		return nil, e
	}
	reason, e := platform.Reason(in.Reason, 20)
	if e != nil {
		return nil, platform.Fail(422, "validation", "Break-glass reason must be at least 20 characters")
	}
	mins := in.DurationMinutes
	if mins == 0 {
		mins = 60
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		create := func() (Raw, error) {
			var id string
			if e := tx.QueryRow(ctx, `SELECT iam.open_break_glass($1::uuid,$2,$3)::text`, patientID, reason, mins).Scan(&id); e != nil {
				return nil, e
			}
			return platform.One(ctx, tx, `SELECT `+bgJ+` FROM iam.break_glass_access b WHERE b.id=$1::uuid`, id)
		}
		if key != "" {
			return platform.Idempotent(ctx, tx, sess, "break_glass.open:"+patientID, key, map[string]any{"reason": reason, "mins": mins}, create)
		}
		return create()
	})
}

func (s Service) EndBreakGlass(ctx context.Context, a identity.Actor, id, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "break_glass.use", "audit.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE iam.break_glass_access SET ended_at=now() WHERE id=$1::uuid AND membership_id=$2::uuid AND ended_at IS NULL`, id, sess.MembershipID)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "iam.break_glass_access", id); e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "iam.break_glass.end", "phi_access", "success", "break_glass", id, "", ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+bgJ+` FROM iam.break_glass_access b WHERE b.id=$1::uuid`, id)
	})
}

func (s Service) ListBreakGlass(ctx context.Context, a identity.Actor, pendingOnly bool, after, request string, limit int) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "audit.read", "break_glass.use"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		out, e := platform.Many(ctx, tx, `SELECT `+bgJ+` FROM iam.break_glass_access b
 WHERE ($1=false OR b.reviewed_at IS NULL)
   AND ($2='' OR b.id>NULLIF($2,'')::uuid)
 ORDER BY b.started_at DESC, b.id LIMIT $3`, pendingOnly, after, limit)
		if e != nil {
			return nil, e
		}
		return out, platform.Audit(ctx, tx, "privacy.break_glass.list", "security", "success", "break_glass", "", "", "")
	})
}

func (s Service) ReviewBreakGlass(ctx context.Context, a identity.Actor, id string, in BreakGlassReview, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "audit.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE iam.break_glass_access SET reviewed_by=$2::uuid,reviewed_at=now(),review_outcome=$3,review_note=NULLIF($4,'')
 WHERE id=$1::uuid AND reviewed_at IS NULL`, id, sess.UserID, in.Outcome, strings.TrimSpace(in.Note))
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "iam.break_glass_access", id); e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "privacy.break_glass.review", "security", "success", "break_glass", id, "", in.Outcome); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+bgJ+` FROM iam.break_glass_access b WHERE b.id=$1::uuid`, id)
	})
}

// ---------------------------------------------------------------- patient PHI access report

func (s Service) PatientAccess(ctx context.Context, a identity.Actor, patientID string, before int64, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "audit.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		out, e := platform.Many(ctx, tx, `SELECT jsonb_build_object(
 'id',e.id,'occurred_at',e.occurred_at,'actor_type',e.actor_type,'actor_user_id',e.actor_user_id,
 'action',e.action,'category',e.category,'resource_type',e.resource_type,'resource_id',e.resource_id,
 'patient_id',e.patient_id,'outcome',e.outcome,'reason',e.reason,'request_id',e.request_id)
 FROM audit.event e WHERE e.patient_id=$1::uuid AND e.id<$2 ORDER BY e.id DESC LIMIT 100`, patientID, before)
		if e != nil {
			return nil, e
		}
		return out, platform.Audit(ctx, tx, "privacy.patient_access.view", "security", "success", "patient", patientID, patientID, "")
	})
}

// ---------------------------------------------------------------- privacy alerts

type AlertReviewInput struct {
	Status  string `json:"status" validate:"required,oneof=acknowledged resolved false_positive"`
	Note    string `json:"note" validate:"max=1000"`
	Version int    `json:"version" validate:"required,min=1"`
}

func (s Service) Alerts(ctx context.Context, a identity.Actor, status, after, request string, limit int) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "audit.read", "privacy.request.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		out, e := platform.Many(ctx, tx, `SELECT `+alertJ+` FROM gov.privacy_alert a
 WHERE ($1='' OR a.status=$1) AND ($2='' OR a.id>NULLIF($2,'')::uuid)
 ORDER BY a.created_at DESC, a.id LIMIT $3`, status, after, limit)
		if e != nil {
			return nil, e
		}
		return out, platform.Audit(ctx, tx, "privacy.alerts.list", "security", "success", "privacy_alert", "", "", "")
	})
}

func (s Service) ReviewAlert(ctx context.Context, a identity.Actor, id string, in AlertReviewInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "audit.read", "privacy.request.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE gov.privacy_alert SET status=$3,reviewed_by=$4::uuid,reviewed_at=now(),review_note=NULLIF($5,'')
 WHERE id=$1::uuid AND row_version=$2`, id, in.Version, in.Status, sess.UserID, strings.TrimSpace(in.Note))
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "gov.privacy_alert", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+alertJ+` FROM gov.privacy_alert a WHERE a.id=$1::uuid`, id)
	})
}

func (s Service) DetectAlerts(ctx context.Context, a identity.Actor, readThreshold, mfaThreshold int, request string) (int, error) {
	if e := platform.Need(a.Permissions, "audit.read", "privacy.request.manage"); e != nil {
		return 0, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (int, error) {
		var n int
		e := tx.QueryRow(ctx, `SELECT gov.detect_privacy_alerts($1,$2)`, readThreshold, mfaThreshold).Scan(&n)
		return n, e
	})
}

// ---------------------------------------------------------------- data-subject privacy requests

type PrivacyRequestInput struct {
	PatientID   string  `json:"patient_id" validate:"required,uuid"`
	RequestType string  `json:"request_type" validate:"required,oneof=access copy_export correction restriction objection erasure accounting_of_disclosures"`
	ReceivedVia string  `json:"received_via" validate:"required,oneof=in_person portal email letter"`
	DueAt       *string `json:"due_at" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
}

type PrivacyRequestStatus struct {
	Status           string  `json:"status" validate:"required,oneof=identity_verified in_progress fulfilled partially_fulfilled denied withdrawn"`
	OutcomeNote      string  `json:"outcome_note" validate:"max=1000"`
	DenialReason     string  `json:"denial_reason" validate:"max=1000"`
	ExportDocumentID *string `json:"export_document_id" validate:"omitempty,uuid"`
	Version          int     `json:"version" validate:"required,min=1"`
}

func (s Service) CreatePrivacyRequest(ctx context.Context, a identity.Actor, in PrivacyRequestInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "privacy.request.manage"); e != nil {
		return nil, e
	}
	due := time.Now().UTC().AddDate(0, 0, 30)
	if in.DueAt != nil {
		t, e := time.Parse(time.RFC3339, *in.DueAt)
		if e != nil {
			return nil, platform.Fail(422, "validation", "Invalid due_at")
		}
		due = t.UTC()
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO gov.privacy_request(org_id,patient_id,request_type,received_via,due_at,handled_by)
 VALUES($1::uuid,$2::uuid,$3,$4,$5,$6::uuid) RETURNING id::text`,
			sess.OrgID, in.PatientID, in.RequestType, in.ReceivedVia, due, sess.UserID).Scan(&id)
		if e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+reqJ+` FROM gov.privacy_request r WHERE r.id=$1::uuid`, id)
	})
}

func (s Service) ListPrivacyRequests(ctx context.Context, a identity.Actor, status, patientID, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "privacy.request.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+reqJ+` FROM gov.privacy_request r
 WHERE ($1='' OR r.status=$1) AND ($2='' OR r.patient_id=NULLIF($2,'')::uuid)
 ORDER BY r.received_at DESC LIMIT 100`, status, patientID)
	})
}

func (s Service) UpdatePrivacyRequestStatus(ctx context.Context, a identity.Actor, id string, in PrivacyRequestStatus, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "privacy.request.manage"); e != nil {
		return nil, e
	}
	if in.Status == "denied" && strings.TrimSpace(in.DenialReason) == "" {
		return nil, platform.Fail(422, "validation", "denial_reason is required when denying a privacy request")
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE gov.privacy_request SET
 status=$3,
 identity_verified_by=CASE WHEN $3='identity_verified' THEN $4::uuid ELSE identity_verified_by END,
 handled_by=$4::uuid,
 outcome_note=CASE WHEN $5<>'' THEN $5 ELSE outcome_note END,
 denial_reason=CASE WHEN $6<>'' THEN $6 ELSE denial_reason END,
 export_document_id=coalesce($7::uuid,export_document_id),
 closed_at=CASE WHEN $3 IN ('fulfilled','partially_fulfilled','denied','withdrawn') THEN now() ELSE closed_at END
 WHERE id=$1::uuid AND row_version=$2`,
			id, in.Version, in.Status, sess.UserID, strings.TrimSpace(in.OutcomeNote), strings.TrimSpace(in.DenialReason), in.ExportDocumentID)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "gov.privacy_request", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+reqJ+` FROM gov.privacy_request r WHERE r.id=$1::uuid`, id)
	})
}

// ---------------------------------------------------------------- legal holds

type LegalHoldInput struct {
	Scope     string  `json:"scope" validate:"required,oneof=patient org_wide"`
	PatientID *string `json:"patient_id" validate:"omitempty,uuid"`
	Reason    string  `json:"reason" validate:"required,min=5,max=1000"`
	Reference string  `json:"reference" validate:"max=160"`
}

func (s Service) PlaceLegalHold(ctx context.Context, a identity.Actor, in LegalHoldInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "privacy.request.manage"); e != nil {
		return nil, e
	}
	if (in.Scope == "patient") != (in.PatientID != nil && *in.PatientID != "") {
		return nil, platform.Fail(422, "validation", "patient_id is required iff scope is patient")
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO gov.legal_hold(org_id,patient_id,scope,reason,reference)
 VALUES($1::uuid,$2::uuid,$3,$4,NULLIF($5,'')) RETURNING id::text`,
			sess.OrgID, in.PatientID, in.Scope, strings.TrimSpace(in.Reason), strings.TrimSpace(in.Reference)).Scan(&id)
		if e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+holdJ+` FROM gov.legal_hold h WHERE h.id=$1::uuid`, id)
	})
}

func (s Service) ListLegalHolds(ctx context.Context, a identity.Actor, activeOnly bool, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "privacy.request.manage", "audit.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+holdJ+` FROM gov.legal_hold h
 WHERE ($1=false OR h.released_at IS NULL) ORDER BY h.placed_at DESC LIMIT 100`, activeOnly)
	})
}

func (s Service) ReleaseLegalHold(ctx context.Context, a identity.Actor, id, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "privacy.request.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE gov.legal_hold SET released_by=$2::uuid,released_at=now() WHERE id=$1::uuid AND released_at IS NULL`, id, sess.UserID)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "gov.legal_hold", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+holdJ+` FROM gov.legal_hold h WHERE h.id=$1::uuid`, id)
	})
}

// ---------------------------------------------------------------- periodic access reviews

type AccessReviewInput struct {
	Title       string `json:"title" validate:"required,min=3,max=200"`
	PeriodStart string `json:"period_start" validate:"required,datetime=2006-01-02"`
	PeriodEnd   string `json:"period_end" validate:"required,datetime=2006-01-02"`
}

type AccessReviewDecision struct {
	Decision string `json:"decision" validate:"required,oneof=keep revoke"`
	Note     string `json:"note" validate:"max=500"`
}

type AccessReviewComplete struct {
	Status  string `json:"status" validate:"required,oneof=completed cancelled"`
	Version int    `json:"version" validate:"required,min=1"`
}

func (s Service) CreateAccessReview(ctx context.Context, a identity.Actor, in AccessReviewInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "admin.access_review"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO iam.access_review(org_id,title,period_start,period_end)
 VALUES($1::uuid,$2,$3::date,$4::date) RETURNING id::text`,
			sess.OrgID, strings.TrimSpace(in.Title), in.PeriodStart, in.PeriodEnd).Scan(&id)
		if e != nil {
			return nil, e
		}
		// Populate items from every active, unrevoked membership_role in this org.
		if _, e = tx.Exec(ctx, `INSERT INTO iam.access_review_item(org_id,review_id,membership_role_id)
 SELECT $1::uuid,$2::uuid,mr.id FROM iam.membership_role mr
 WHERE mr.org_id=$1::uuid AND mr.revoked_at IS NULL AND (mr.expires_at IS NULL OR mr.expires_at>now())`,
			sess.OrgID, id); e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "iam.access_review.create", "administration", "success", "access_review", id, "", ""); e != nil {
			return nil, e
		}
		return loadAccessReview(ctx, tx, id)
	})
}

func loadAccessReview(ctx context.Context, tx pgx.Tx, id string) (Raw, error) {
	return platform.One(ctx, tx, `SELECT `+reviewJ+` || jsonb_build_object('items',
 coalesce((SELECT jsonb_agg(`+revItemJ+` || jsonb_build_object('role_code',ro.code,'user_id',m.user_id,'display_name',u.display_name,'email',u.email) ORDER BY u.display_name, ro.code)
  FROM iam.access_review_item i
  JOIN iam.membership_role mr ON mr.id=i.membership_role_id
  JOIN iam.role ro ON ro.id=mr.role_id
  JOIN iam.membership m ON m.id=mr.membership_id
  JOIN iam.user_account u ON u.id=m.user_id
  WHERE i.review_id=r.id),'[]'::jsonb))
 FROM iam.access_review r WHERE r.id=$1::uuid`, id)
}

func (s Service) ListAccessReviews(ctx context.Context, a identity.Actor, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "admin.access_review", "audit.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+reviewJ+` FROM iam.access_review r ORDER BY r.created_at DESC LIMIT 50`)
	})
}

func (s Service) GetAccessReview(ctx context.Context, a identity.Actor, id, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "admin.access_review", "audit.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		return loadAccessReview(ctx, tx, id)
	})
}

func (s Service) DecideAccessReviewItem(ctx context.Context, a identity.Actor, reviewID, itemID string, in AccessReviewDecision, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "admin.access_review"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var mrID string
		e := tx.QueryRow(ctx, `UPDATE iam.access_review_item SET decision=$3,decided_by=$4::uuid,decided_at=now(),note=NULLIF($5,'')
 WHERE id=$1::uuid AND review_id=$2::uuid RETURNING membership_role_id::text`,
			itemID, reviewID, in.Decision, sess.UserID, strings.TrimSpace(in.Note)).Scan(&mrID)
		if e != nil {
			return nil, e
		}
		if in.Decision == "revoke" && a.Can("admin.user.manage") {
			_, _ = tx.Exec(ctx, `UPDATE iam.membership_role SET revoked_at=now(),revoked_by=$2::uuid,revoke_reason=coalesce(NULLIF($3,''),'revoked in access review')
 WHERE id=$1::uuid AND revoked_at IS NULL`, mrID, sess.UserID, strings.TrimSpace(in.Note))
		}
		return platform.One(ctx, tx, `SELECT `+revItemJ+` FROM iam.access_review_item i WHERE i.id=$1::uuid`, itemID)
	})
}

func (s Service) CompleteAccessReview(ctx context.Context, a identity.Actor, id string, in AccessReviewComplete, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "admin.access_review"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		if in.Status == "completed" {
			var pending int
			if e := tx.QueryRow(ctx, `SELECT count(*) FROM iam.access_review_item WHERE review_id=$1::uuid AND decision='pending'`, id).Scan(&pending); e != nil {
				return nil, e
			}
			if pending > 0 {
				return nil, platform.Fail(409, "pending_items", "All access review items must be decided before completing")
			}
		}
		tag, e := tx.Exec(ctx, `UPDATE iam.access_review SET status=$3,completed_at=CASE WHEN $3='completed' THEN now() ELSE NULL END
 WHERE id=$1::uuid AND row_version=$2 AND status='open'`, id, in.Version, in.Status)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "iam.access_review", id); e != nil {
			return nil, e
		}
		return loadAccessReview(ctx, tx, id)
	})
}
