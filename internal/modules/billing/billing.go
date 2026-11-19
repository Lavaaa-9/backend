// Package billing covers payers, the price list, invoices, payments, receipts, cashier shifts and refunds.
// Money is exchanged as JSON numbers with two decimals and stored as numeric(14,2).
package billing

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
)

type Service struct {
	DB       *pgxpool.Pool
	Keys     *security.Keyring
	IndexKey []byte
}
type Raw = platform.Raw

type VersionInput struct {
	Version int `json:"version" validate:"required,min=1"`
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- payers

type PayerInput struct {
	Code      string `json:"code" validate:"required,max=30"`
	Name      string `json:"name" validate:"required,min=2,max=160"`
	PayerType string `json:"payer_type" validate:"required,oneof=self_pay insurance corporate government"`
}

func (s Service) Payers(ctx context.Context, a identity.Actor, request string) ([]Raw, error) {
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+platform.JSON("p", true)+` FROM billing.payer p WHERE status='active' ORDER BY name`)
	})
}

func (s Service) CreatePayer(ctx context.Context, a identity.Actor, in PayerInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.price.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		return platform.One(ctx, tx, `WITH p AS (INSERT INTO billing.payer(org_id,code,name,payer_type) VALUES($1::uuid,$2,$3,$4) RETURNING *) SELECT `+platform.JSON("p", true)+` FROM p`,
			sess.OrgID, strings.ToUpper(strings.TrimSpace(in.Code)), strings.TrimSpace(in.Name), in.PayerType)
	})
}

// ---------------------------------------------------------------- services and prices

type ServiceInput struct {
	Code       string  `json:"code" validate:"required,max=30"`
	Name       string  `json:"name" validate:"required,min=2,max=160"`
	Category   string  `json:"category" validate:"required,oneof=registration consultation laboratory imaging pharmacy procedure telemedicine other"`
	DiagTestID *string `json:"diag_test_id" validate:"omitempty,uuid"`
	DrugID     *string `json:"drug_id" validate:"omitempty,uuid"`
}
type PriceInput struct {
	PayerID       *string `json:"payer_id" validate:"omitempty,uuid"`
	UnitPrice     float64 `json:"unit_price" validate:"min=0,lte=1000000000"`
	EffectiveFrom *string `json:"effective_from" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
	EffectiveTo   *string `json:"effective_to" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
}

const svcSQL = `SELECT (to_jsonb(s) - 'org_id' - 'created_by' - 'updated_by' - 'row_version') || jsonb_build_object('version',s.row_version,'current_price',billing.price_for(s.id,NULL)) FROM billing.service_catalog s`

func (s Service) Services(ctx context.Context, a identity.Actor, category, request string) ([]Raw, error) {
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, svcSQL+` WHERE s.status='active' AND ($1='' OR s.category=$1) ORDER BY s.name LIMIT 300`, category)
	})
}

func (s Service) CreateService(ctx context.Context, a identity.Actor, in ServiceInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.price.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO billing.service_catalog(org_id,code,name,category,diag_test_id,drug_id) VALUES($1::uuid,$2,$3,$4,$5::uuid,$6::uuid) RETURNING id::text`,
			sess.OrgID, strings.ToUpper(strings.TrimSpace(in.Code)), strings.TrimSpace(in.Name), in.Category, in.DiagTestID, in.DrugID).Scan(&id)
		if e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, svcSQL+` WHERE s.id=$1::uuid`, id)
	})
}

// SetPrice adds a price valid from a moment (default now) until an optional end. Overlapping prices are refused.
func (s Service) SetPrice(ctx context.Context, a identity.Actor, service string, in PriceInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.price.manage"); e != nil {
		return nil, e
	}
	from := time.Now().UTC()
	var to *time.Time
	if in.EffectiveFrom != nil {
		from, _ = time.Parse(time.RFC3339, *in.EffectiveFrom)
	}
	if in.EffectiveTo != nil {
		t, _ := time.Parse(time.RFC3339, *in.EffectiveTo)
		if !t.After(from) {
			return nil, platform.Fail(422, "validation", "effective_to must be after effective_from")
		}
		to = &t
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		return platform.One(ctx, tx, `WITH p AS (INSERT INTO billing.price(org_id,service_id,payer_id,unit_price,effective) VALUES($1::uuid,$2::uuid,$3::uuid,$4,tstzrange($5,$6,'[)')) RETURNING *)
 SELECT jsonb_build_object('id',p.id,'service_id',p.service_id,'payer_id',p.payer_id,'unit_price',p.unit_price,'effective_from',lower(p.effective),'effective_to',upper(p.effective)) FROM p`,
			sess.OrgID, service, in.PayerID, in.UnitPrice, from, to)
	})
}

func (s Service) Prices(ctx context.Context, a identity.Actor, service, request string) ([]Raw, error) {
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT jsonb_build_object('id',p.id,'service_id',p.service_id,'payer_id',p.payer_id,'unit_price',p.unit_price,'effective_from',lower(p.effective),'effective_to',upper(p.effective))
 FROM billing.price p WHERE service_id=$1::uuid ORDER BY lower(effective) DESC`, service)
	})
}

// ---------------------------------------------------------------- invoices

type InvoiceInput struct {
	EncounterID string  `json:"encounter_id" validate:"required,uuid"`
	PayerID     *string `json:"payer_id" validate:"omitempty,uuid"`
}
type LineInput struct {
	ServiceID         string  `json:"service_id" validate:"required,uuid"`
	SourceType        string  `json:"source_type" validate:"required,oneof=registration consultation diagnostic pharmacy procedure telemedicine manual"`
	Quantity          float64 `json:"quantity" validate:"required,gt=0,lte=100000"`
	Description       string  `json:"description" validate:"max=300"`
	DiscountAmount    float64 `json:"discount_amount" validate:"min=0"`
	DiscountReason    string  `json:"discount_reason" validate:"max=300"`
	ConsultationID    *string `json:"consultation_id" validate:"omitempty,uuid"`
	DiagRequestItemID *string `json:"diag_request_item_id" validate:"omitempty,uuid"`
	DispenseItemID    *string `json:"dispense_item_id" validate:"omitempty,uuid"`
	ProcedureID       *string `json:"procedure_id" validate:"omitempty,uuid"`
}
type VoidInput struct {
	Reason  string `json:"reason" validate:"required,min=5,max=500"`
	Version int    `json:"version" validate:"required,min=1"`
}

const invSQL = `SELECT (to_jsonb(i) - 'org_id' - 'created_by' - 'updated_by' - 'row_version') || jsonb_build_object('version',i.row_version,
 'lines',coalesce((SELECT jsonb_agg((to_jsonb(l) - 'org_id' - 'invoice_id') ORDER BY l.id) FROM billing.invoice_line l WHERE l.invoice_id=i.id AND NOT l.voided),'[]'::jsonb),
 'payments',coalesce((SELECT jsonb_agg(jsonb_build_object('payment_id',p.id,'amount',a.amount,'method',p.method,'status',p.status,'received_at',p.received_at,'receipt_no',r.receipt_no) ORDER BY a.allocated_at)
   FROM billing.payment_allocation a JOIN billing.payment p ON p.id=a.payment_id LEFT JOIN billing.receipt r ON r.payment_id=p.id WHERE a.invoice_id=i.id),'[]'::jsonb)) FROM billing.invoice i`

func (s Service) invoice(ctx context.Context, tx pgx.Tx, id string) (Raw, error) {
	return platform.One(ctx, tx, invSQL+` WHERE i.id=$1::uuid`, id)
}

// CreateInvoice opens (or returns the existing) draft invoice for a visit and payer; the default payer is self-pay.
func (s Service) CreateInvoice(ctx context.Context, a identity.Actor, in InvoiceInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.invoice.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		patient, facility, e := platform.Encounter(ctx, tx, a.FacilityIDs, in.EncounterID)
		if e != nil {
			return nil, e
		}
		payer := ""
		if in.PayerID != nil {
			payer = *in.PayerID
		} else if e = tx.QueryRow(ctx, `SELECT id::text FROM billing.payer WHERE payer_type='self_pay' AND status='active' ORDER BY code LIMIT 1`).Scan(&payer); e != nil {
			return nil, platform.Fail(422, "validation", "No self-pay payer is configured; create one first")
		}
		var id string
		e = tx.QueryRow(ctx, `SELECT id::text FROM billing.invoice WHERE encounter_id=$1::uuid AND payer_id=$2::uuid AND status='draft'`, in.EncounterID, payer).Scan(&id)
		if errors.Is(e, pgx.ErrNoRows) {
			e = tx.QueryRow(ctx, `INSERT INTO billing.invoice(org_id,facility_id,patient_id,encounter_id,payer_id) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid) RETURNING id::text`,
				sess.OrgID, facility, patient, in.EncounterID, payer).Scan(&id)
		}
		if e != nil {
			return nil, e
		}
		return s.invoice(ctx, tx, id)
	})
}

func (s Service) Invoices(ctx context.Context, a identity.Actor, status, patient, encounter, after, request string, limit int) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "billing.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		out, e := platform.Many(ctx, tx, invSQL+` WHERE i.facility_id=ANY($1::uuid[]) AND ($2='' OR i.status=$2) AND ($3='' OR i.patient_id=NULLIF($3,'')::uuid)
 AND ($4='' OR i.encounter_id=NULLIF($4,'')::uuid) AND ($5='' OR i.id>NULLIF($5,'')::uuid) ORDER BY i.id LIMIT $6`, a.FacilityIDs, status, patient, encounter, after, limit)
		if e != nil {
			return nil, e
		}
		return out, platform.Audit(ctx, tx, "invoice.list", "phi_access", "success", "invoice", "", patient, "")
	})
}

func (s Service) Invoice(ctx context.Context, a identity.Actor, id, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		out, e := platform.One(ctx, tx, invSQL+` WHERE i.id=$1::uuid AND i.facility_id=ANY($2::uuid[])`, id, a.FacilityIDs)
		if e != nil {
			return nil, e
		}
		var patient string
		tx.QueryRow(ctx, `SELECT patient_id::text FROM billing.invoice WHERE id=$1::uuid`, id).Scan(&patient)
		return out, platform.Audit(ctx, tx, "invoice.view", "phi_access", "success", "invoice", id, patient, "")
	})
}

// draftInvoice loads a draft invoice the actor may change.
func draftInvoice(ctx context.Context, tx pgx.Tx, a identity.Actor, id string) (payer string, err error) {
	var status string
	err = tx.QueryRow(ctx, `SELECT payer_id::text,status FROM billing.invoice WHERE id=$1::uuid AND facility_id=ANY($2::uuid[])`, id, a.FacilityIDs).Scan(&payer, &status)
	if err == nil && status != "draft" {
		err = platform.Fail(409, "conflict", "Lines can only change while the invoice is a draft")
	}
	return
}

func (s Service) AddLine(ctx context.Context, a identity.Actor, id string, in LineInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.invoice.manage"); e != nil {
		return nil, e
	}
	if in.DiscountAmount > 0 && strings.TrimSpace(in.DiscountReason) == "" {
		return nil, platform.Fail(422, "validation", "A discount needs a reason")
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		payer, e := draftInvoice(ctx, tx, a, id)
		if e != nil {
			return nil, e
		}
		var price *float64
		var name string
		if e = tx.QueryRow(ctx, `SELECT billing.price_for(s.id,$2::uuid)::float8,s.name FROM billing.service_catalog s WHERE s.id=$1::uuid`, in.ServiceID, payer).Scan(&price, &name); e != nil {
			return nil, e
		}
		if price == nil {
			return nil, platform.Fail(422, "validation", "No current price for this service and payer")
		}
		desc := strings.TrimSpace(in.Description)
		if desc == "" {
			desc = name
		}
		if _, e = tx.Exec(ctx, `INSERT INTO billing.invoice_line(org_id,invoice_id,service_id,source_type,consultation_id,diag_request_item_id,dispense_item_id,procedure_id,description,quantity,unit_price,discount_amount,discount_reason)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5::uuid,$6::uuid,$7::uuid,$8::uuid,$9,$10,$11,$12,NULLIF($13,''))`, sess.OrgID, id, in.ServiceID, in.SourceType, in.ConsultationID, in.DiagRequestItemID,
			in.DispenseItemID, in.ProcedureID, desc, in.Quantity, *price, in.DiscountAmount, strings.TrimSpace(in.DiscountReason)); e != nil {
			return nil, e
		}
		return s.invoice(ctx, tx, id)
	})
}

// RemoveLine deletes a line from a draft invoice.
func (s Service) RemoveLine(ctx context.Context, a identity.Actor, line, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.invoice.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		var inv string
		if e := tx.QueryRow(ctx, `SELECT invoice_id::text FROM billing.invoice_line WHERE id=$1::uuid`, line).Scan(&inv); e != nil {
			return nil, e
		}
		if _, e := draftInvoice(ctx, tx, a, inv); e != nil {
			return nil, e
		}
		if _, e := tx.Exec(ctx, `DELETE FROM billing.invoice_line WHERE id=$1::uuid`, line); e != nil {
			return nil, e
		}
		return s.invoice(ctx, tx, inv)
	})
}

// CollectCharges adds a line for every priced, not-yet-billed procedure, dispensed drug and diagnostic
// test of the invoice's visit. Items whose service has no price for the payer are skipped.
func (s Service) CollectCharges(ctx context.Context, a identity.Actor, id, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.invoice.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		if _, e := draftInvoice(ctx, tx, a, id); e != nil {
			return nil, e
		}
		if _, e := tx.Exec(ctx, `SELECT billing.collect_charges($1::uuid)`, id); e != nil {
			return nil, e
		}
		return s.invoice(ctx, tx, id)
	})
}

// Issue freezes the draft and allocates the invoice number.
func (s Service) Issue(ctx context.Context, a identity.Actor, id string, in VersionInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.invoice.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE billing.invoice SET status='issued' WHERE id=$1::uuid AND row_version=$2 AND status='draft' AND facility_id=ANY($3::uuid[])`, id, in.Version, a.FacilityIDs)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "billing.invoice", id); e != nil {
			return nil, e
		}
		return s.invoice(ctx, tx, id)
	})
}

func (s Service) VoidInvoice(ctx context.Context, a identity.Actor, id string, in VoidInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.invoice.void"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE billing.invoice SET status='void',void_reason=$3 WHERE id=$1::uuid AND row_version=$2 AND status<>'void' AND facility_id=ANY($4::uuid[])`, id, in.Version, strings.TrimSpace(in.Reason), a.FacilityIDs)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "billing.invoice", id); e != nil {
			return nil, e
		}
		return s.invoice(ctx, tx, id)
	})
}

// ---------------------------------------------------------------- cashier shifts

type ShiftOpen struct {
	FacilityID   string  `json:"facility_id" validate:"required,uuid"`
	OpeningFloat float64 `json:"opening_float" validate:"min=0"`
}
type ShiftClose struct {
	CountedCash float64 `json:"counted_cash" validate:"min=0"`
	Version     int     `json:"version" validate:"required,min=1"`
}
type ShiftReconcile struct {
	VarianceNote string `json:"variance_note" validate:"max=500"`
	Version      int    `json:"version" validate:"required,min=1"`
}

var shiftJ = platform.JSON("s", true, "reconciled_by")

func (s Service) OpenShift(ctx context.Context, a identity.Actor, in ShiftOpen, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.payment.record"); e != nil {
		return nil, e
	}
	if !contains(a.FacilityIDs, in.FacilityID) {
		return nil, platform.Forbidden()
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		return platform.One(ctx, tx, `WITH s AS (INSERT INTO billing.cashier_shift(org_id,facility_id,membership_id,opening_float) VALUES($1::uuid,$2::uuid,$3::uuid,$4) RETURNING *) SELECT `+shiftJ+` FROM s`,
			sess.OrgID, in.FacilityID, sess.MembershipID, in.OpeningFloat)
	})
}

func (s Service) CloseShift(ctx context.Context, a identity.Actor, id string, in ShiftClose, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.payment.record"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE billing.cashier_shift SET status='closed',counted_cash=$3 WHERE id=$1::uuid AND row_version=$2 AND status='open' AND membership_id=$4::uuid`, id, in.Version, in.CountedCash, sess.MembershipID)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "billing.cashier_shift", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+shiftJ+` FROM billing.cashier_shift s WHERE id=$1::uuid`, id)
	})
}

// ReconcileShift is done by someone other than the cashier; a variance needs a note.
func (s Service) ReconcileShift(ctx context.Context, a identity.Actor, id string, in ShiftReconcile, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.shift.reconcile"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE billing.cashier_shift SET status='reconciled',reconciled_by=$3::uuid,variance_note=NULLIF($4,'') WHERE id=$1::uuid AND row_version=$2 AND status='closed'`,
			id, in.Version, sess.UserID, strings.TrimSpace(in.VarianceNote))
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "billing.cashier_shift", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+shiftJ+` FROM billing.cashier_shift s WHERE id=$1::uuid`, id)
	})
}

func (s Service) Shifts(ctx context.Context, a identity.Actor, status, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "billing.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+shiftJ+` FROM billing.cashier_shift s WHERE s.facility_id=ANY($1::uuid[]) AND ($2='' OR s.status=$2) ORDER BY s.opened_at DESC LIMIT 100`, a.FacilityIDs, status)
	})
}

// ---------------------------------------------------------------- payments

type PaymentInput struct {
	InvoiceID         string  `json:"invoice_id" validate:"required,uuid"`
	Amount            float64 `json:"amount" validate:"required,gt=0,lte=1000000000"`
	Method            string  `json:"method" validate:"required,oneof=cash mobile_money card bank_transfer insurance"`
	Provider          *string `json:"provider" validate:"omitempty,oneof=mpesa airtel_money mixx_by_yas halopesa selcom azampay bank nhif other"`
	ProviderReference *string `json:"provider_reference" validate:"omitempty,min=3,max=120"`
}
type ReverseInput struct {
	Reason  string `json:"reason" validate:"required,min=5,max=500"`
	Version int    `json:"version" validate:"required,min=1"`
}

const paySQL = `SELECT (to_jsonb(p) - 'org_id' - 'created_by' - 'updated_by' - 'row_version' - 'received_by_membership_id') || jsonb_build_object('version',p.row_version,
 'receipt',(SELECT jsonb_build_object('id',r.id,'receipt_no',r.receipt_no,'issued_at',r.issued_at,'print_count',r.print_count) FROM billing.receipt r WHERE r.payment_id=p.id),
 'allocations',coalesce((SELECT jsonb_agg(jsonb_build_object('invoice_id',a.invoice_id,'invoice_no',i.invoice_no,'amount',a.amount,'invoice_status',i.status,'invoice_balance',i.balance)) FROM billing.payment_allocation a JOIN billing.invoice i ON i.id=a.invoice_id WHERE a.payment_id=p.id),'[]'::jsonb)) FROM billing.payment p`

// RecordPayment takes a payment against an issued invoice and issues the receipt. Cash goes into the
// cashier's open shift. The Idempotency-Key header must be a UUID; a replay returns the original payment.
func (s Service) RecordPayment(ctx context.Context, a identity.Actor, in PaymentInput, key, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.payment.record"); e != nil {
		return nil, e
	}
	k, e := uuid.Parse(key)
	if e != nil {
		return nil, platform.Fail(422, "idempotency_key_required", "Idempotency-Key must be a UUID")
	}
	if in.Method != "cash" && in.Method != "insurance" && (in.Provider == nil || in.ProviderReference == nil) {
		return nil, platform.Fail(422, "validation", "provider and provider_reference are required for electronic payments")
	}
	if in.Method == "insurance" && in.ProviderReference == nil {
		return nil, platform.Fail(422, "validation", "provider_reference is required for insurance payments")
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var id string
		e := tx.QueryRow(ctx, `SELECT id::text FROM billing.payment WHERE idempotency_key=$1::uuid`, k.String()).Scan(&id)
		if e == nil {
			return platform.One(ctx, tx, paySQL+` WHERE p.id=$1::uuid`, id)
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return nil, e
		}
		var patient, facility, status string
		if e = tx.QueryRow(ctx, `SELECT patient_id::text,facility_id::text,status FROM billing.invoice WHERE id=$1::uuid AND facility_id=ANY($2::uuid[])`, in.InvoiceID, a.FacilityIDs).Scan(&patient, &facility, &status); e != nil {
			return nil, e
		}
		if status != "issued" && status != "partially_paid" {
			return nil, platform.Fail(409, "conflict", "Invoice is "+status+" and cannot accept payment")
		}
		var shift *string
		if in.Method == "cash" {
			var sid string
			if e = tx.QueryRow(ctx, `SELECT id::text FROM billing.cashier_shift WHERE membership_id=$1::uuid AND status='open' AND facility_id=$2::uuid`, sess.MembershipID, facility).Scan(&sid); e != nil {
				if errors.Is(e, pgx.ErrNoRows) {
					return nil, platform.Fail(409, "no_open_shift", "Open a cashier shift for this facility before taking cash")
				}
				return nil, e
			}
			shift = &sid
		}
		if e = tx.QueryRow(ctx, `INSERT INTO billing.payment(org_id,facility_id,patient_id,method,provider,provider_reference,amount,status,idempotency_key,cashier_shift_id,received_by_membership_id)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5,$6,$7,'completed',$8::uuid,$9::uuid,$10::uuid) RETURNING id::text`, sess.OrgID, facility, patient, in.Method, in.Provider, in.ProviderReference, in.Amount, k.String(), shift, sess.MembershipID).Scan(&id); e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO billing.payment_allocation(org_id,payment_id,invoice_id,amount) VALUES($1::uuid,$2::uuid,$3::uuid,$4)`, sess.OrgID, id, in.InvoiceID, in.Amount); e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "payment.record", "phi_access", "success", "payment", id, patient, ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, paySQL+` WHERE p.id=$1::uuid`, id)
	})
}

func (s Service) Payment(ctx context.Context, a identity.Actor, id, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		return platform.One(ctx, tx, paySQL+` WHERE p.id=$1::uuid AND p.facility_id=ANY($2::uuid[])`, id, a.FacilityIDs)
	})
}

func (s Service) ReversePayment(ctx context.Context, a identity.Actor, id string, in ReverseInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.payment.reverse"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE billing.payment SET status='reversed',reversal_reason=$3 WHERE id=$1::uuid AND row_version=$2 AND status='completed' AND facility_id=ANY($4::uuid[])`, id, in.Version, strings.TrimSpace(in.Reason), a.FacilityIDs)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "billing.payment", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, paySQL+` WHERE p.id=$1::uuid`, id)
	})
}

// PrintReceipt counts a receipt print and returns the receipt with its payment.
func (s Service) PrintReceipt(ctx context.Context, a identity.Actor, payment, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE billing.receipt SET print_count=print_count+1 WHERE payment_id=$1::uuid`, payment)
		if e != nil {
			return nil, e
		}
		if tag.RowsAffected() != 1 {
			return nil, platform.NotFound()
		}
		return platform.One(ctx, tx, paySQL+` WHERE p.id=$1::uuid AND p.facility_id=ANY($2::uuid[])`, payment, a.FacilityIDs)
	})
}

// ---------------------------------------------------------------- refunds (four-eyes)

type RefundInput struct {
	Amount float64 `json:"amount" validate:"required,gt=0"`
	Reason string  `json:"reason" validate:"required,min=10,max=500"`
}
type RefundDecision struct {
	Approve bool `json:"approve"`
	Version int  `json:"version" validate:"required,min=1"`
}
type RefundPaid struct {
	PayoutReference string `json:"payout_reference" validate:"required,min=3,max=120"`
	Version         int    `json:"version" validate:"required,min=1"`
}

var refundJ = platform.JSON("r", true)

func (s Service) RequestRefund(ctx context.Context, a identity.Actor, payment string, in RefundInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.refund.request"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var ok bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM billing.payment WHERE id=$1::uuid AND facility_id=ANY($2::uuid[]))`, payment, a.FacilityIDs).Scan(&ok); e != nil {
			return nil, e
		}
		if !ok {
			return nil, platform.NotFound()
		}
		return platform.One(ctx, tx, `WITH r AS (INSERT INTO billing.refund(org_id,payment_id,amount,reason) VALUES($1::uuid,$2::uuid,$3,$4) RETURNING *) SELECT `+refundJ+` FROM r`,
			sess.OrgID, payment, in.Amount, strings.TrimSpace(in.Reason))
	})
}

func (s Service) Refunds(ctx context.Context, a identity.Actor, status, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "billing.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+refundJ+` FROM billing.refund r JOIN billing.payment p ON p.id=r.payment_id WHERE p.facility_id=ANY($1::uuid[]) AND ($2='' OR r.status=$2) ORDER BY r.created_at DESC LIMIT 100`, a.FacilityIDs, status)
	})
}

// DecideRefund approves or rejects a refund; the approver cannot be the requester.
func (s Service) DecideRefund(ctx context.Context, a identity.Actor, id string, in RefundDecision, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.refund.approve"); e != nil {
		return nil, e
	}
	status := "rejected"
	if in.Approve {
		status = "approved"
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE billing.refund SET status=$3,approved_by=$4::uuid WHERE id=$1::uuid AND row_version=$2 AND status='requested'`, id, in.Version, status, sess.UserID)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "billing.refund", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+refundJ+` FROM billing.refund r WHERE id=$1::uuid`, id)
	})
}

func (s Service) PayRefund(ctx context.Context, a identity.Actor, id string, in RefundPaid, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "billing.refund.approve"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE billing.refund SET status='paid',payout_reference=$3,paid_at=now() WHERE id=$1::uuid AND row_version=$2 AND status='approved'`, id, in.Version, strings.TrimSpace(in.PayoutReference))
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "billing.refund", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+refundJ+` FROM billing.refund r WHERE id=$1::uuid`, id)
	})
}
