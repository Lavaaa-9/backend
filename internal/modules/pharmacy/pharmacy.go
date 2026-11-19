// Package pharmacy covers the drug catalogue, stock, prescriptions and dispensing.
package pharmacy

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
)

type Service struct{ DB *pgxpool.Pool }
type Raw = platform.Raw

var (
	drugJ  = platform.JSON("d", true)
	batchJ = platform.JSON("b", true)
)

// ---------------------------------------------------------------- catalogue

type DrugInput struct {
	Code         string  `json:"code" validate:"required,max=30"`
	GenericName  string  `json:"generic_name" validate:"required,min=2,max=160"`
	BrandName    *string `json:"brand_name" validate:"omitempty,max=160"`
	DosageForm   string  `json:"dosage_form" validate:"required,oneof=tablet capsule syrup suspension injection infusion cream ointment drops inhaler suppository powder other"`
	Strength     string  `json:"strength" validate:"required,max=60"`
	UnitOfIssue  string  `json:"unit_of_issue" validate:"required,max=40"`
	ATCCode      *string `json:"atc_code" validate:"omitempty,len=7"`
	IsControlled bool    `json:"is_controlled"`
}

func (s Service) Drugs(ctx context.Context, a identity.Actor, q, request string) ([]Raw, error) {
	q = strings.TrimSpace(q)
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+drugJ+` FROM pharmacy.drug d WHERE d.status='active' AND ($1='' OR strpos(lower(d.generic_name||' '||coalesce(d.brand_name,'')||' '||d.code),lower($1))>0) ORDER BY d.generic_name LIMIT 100`, q)
	})
}

func (s Service) CreateDrug(ctx context.Context, a identity.Actor, in DrugInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "pharmacy.catalog.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO pharmacy.drug(org_id,code,generic_name,brand_name,dosage_form,strength,unit_of_issue,atc_code,is_controlled)
 VALUES($1::uuid,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id::text`, sess.OrgID, strings.ToUpper(strings.TrimSpace(in.Code)), strings.TrimSpace(in.GenericName), in.BrandName,
			in.DosageForm, strings.TrimSpace(in.Strength), strings.TrimSpace(in.UnitOfIssue), in.ATCCode, in.IsControlled).Scan(&id)
		if e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+drugJ+` FROM pharmacy.drug d WHERE id=$1::uuid`, id)
	})
}

// ---------------------------------------------------------------- stock

type BatchInput struct {
	FacilityID  string   `json:"facility_id" validate:"required,uuid"`
	DrugID      string   `json:"drug_id" validate:"required,uuid"`
	BatchNo     string   `json:"batch_no" validate:"required,min=1,max=60"`
	ExpiryDate  string   `json:"expiry_date" validate:"required,datetime=2006-01-02"`
	UnitCost    *float64 `json:"unit_cost" validate:"omitempty,min=0"`
	Quantity    float64  `json:"quantity" validate:"required,gt=0,lte=1000000"`
	SupplierRef string   `json:"supplier_ref" validate:"max=120"`
}
type AdjustInput struct {
	MovementType string  `json:"movement_type" validate:"required,oneof=adjustment_up adjustment_down expiry_writeoff return_to_supplier"`
	Quantity     float64 `json:"quantity" validate:"required,gt=0,lte=1000000"`
	Reason       string  `json:"reason" validate:"required,min=3,max=500"`
}

func canStock(a identity.Actor) error {
	return platform.Need(a.Permissions, "pharmacy.read", "pharmacy.stock.manage")
}

const batchSQL = `SELECT ` + `(to_jsonb(b) - 'org_id' - 'created_by' - 'updated_by' - 'row_version')` + ` || jsonb_build_object('version',b.row_version,'drug_name',d.generic_name,'strength',d.strength,'drug_code',d.code) FROM pharmacy.stock_batch b JOIN pharmacy.drug d ON d.id=b.drug_id`

func (s Service) Stock(ctx context.Context, a identity.Actor, facility, drug string, request string) ([]Raw, error) {
	if e := canStock(a); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, batchSQL+` WHERE b.facility_id=ANY($1::uuid[]) AND ($2='' OR b.facility_id=NULLIF($2,'')::uuid) AND ($3='' OR b.drug_id=NULLIF($3,'')::uuid)
 ORDER BY d.generic_name,b.expiry_date LIMIT 500`, a.FacilityIDs, facility, drug)
	})
}

// Receive books stock in: the batch is created if new, and a receipt movement raises its balance.
func (s Service) Receive(ctx context.Context, a identity.Actor, in BatchInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "pharmacy.stock.manage"); e != nil {
		return nil, e
	}
	if !contains(a.FacilityIDs, in.FacilityID) {
		return nil, platform.Forbidden()
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO pharmacy.stock_batch(org_id,facility_id,drug_id,batch_no,expiry_date,unit_cost) VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5::date,$6)
 ON CONFLICT (facility_id,drug_id,batch_no) DO UPDATE SET unit_cost=coalesce(EXCLUDED.unit_cost,pharmacy.stock_batch.unit_cost) WHERE pharmacy.stock_batch.expiry_date=EXCLUDED.expiry_date RETURNING id::text`,
			sess.OrgID, in.FacilityID, in.DrugID, strings.TrimSpace(in.BatchNo), in.ExpiryDate, in.UnitCost).Scan(&id)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil, platform.Fail(409, "conflict", "Batch number exists with a different expiry date")
		}
		if e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO pharmacy.stock_movement(org_id,batch_id,movement_type,quantity,supplier_ref) VALUES($1::uuid,$2::uuid,'receipt',$3,NULLIF($4,''))`, sess.OrgID, id, in.Quantity, strings.TrimSpace(in.SupplierRef)); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, batchSQL+` WHERE b.id=$1::uuid`, id)
	})
}

func contains(l []string, v string) bool {
	for _, x := range l {
		if x == v {
			return true
		}
	}
	return false
}

func (s Service) Adjust(ctx context.Context, a identity.Actor, batch string, in AdjustInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "pharmacy.stock.manage"); e != nil {
		return nil, e
	}
	qty := -in.Quantity
	if in.MovementType == "adjustment_up" {
		qty = in.Quantity
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var ok bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pharmacy.stock_batch WHERE id=$1::uuid AND facility_id=ANY($2::uuid[]))`, batch, a.FacilityIDs).Scan(&ok); e != nil {
			return nil, e
		}
		if !ok {
			return nil, platform.NotFound()
		}
		if _, e := tx.Exec(ctx, `INSERT INTO pharmacy.stock_movement(org_id,batch_id,movement_type,quantity,reason) VALUES($1::uuid,$2::uuid,$3,$4,$5)`, sess.OrgID, batch, in.MovementType, qty, strings.TrimSpace(in.Reason)); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, batchSQL+` WHERE b.id=$1::uuid`, batch)
	})
}

func (s Service) Movements(ctx context.Context, a identity.Actor, batch, request string) ([]Raw, error) {
	if e := canStock(a); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT to_jsonb(m)-'org_id'-'performed_by' FROM pharmacy.stock_movement m JOIN pharmacy.stock_batch b ON b.id=m.batch_id WHERE m.batch_id=$1::uuid AND b.facility_id=ANY($2::uuid[]) ORDER BY m.performed_at DESC LIMIT 200`, batch, a.FacilityIDs)
	})
}

// ---------------------------------------------------------------- prescriptions

type ItemInput struct {
	DrugID             *string `json:"drug_id" validate:"omitempty,uuid"`
	DrugNameText       *string `json:"drug_name_text" validate:"omitempty,min=2,max=200"`
	DoseAmount         float64 `json:"dose_amount" validate:"required,gt=0"`
	DoseUnit           string  `json:"dose_unit" validate:"required,max=30"`
	Route              string  `json:"route" validate:"required,oneof=oral iv im sc topical inhaled rectal vaginal ophthalmic otic nasal sublingual other"`
	FrequencyCode      string  `json:"frequency_code" validate:"required,oneof=STAT OD BD TDS QID Q4H Q6H Q8H NOCTE PRN WEEKLY"`
	DurationDays       *int    `json:"duration_days" validate:"omitempty,min=1,max=365"`
	QuantityPrescribed float64 `json:"quantity_prescribed" validate:"required,gt=0"`
	Instructions       string  `json:"instructions" validate:"max=500"`
}
type PrescriptionInput struct {
	Notes string      `json:"notes" validate:"max=1000"`
	Items []ItemInput `json:"items" validate:"required,min=1,max=30,dive"`
	Sign  bool        `json:"sign"`
}
type VersionInput struct {
	Version int `json:"version" validate:"required,min=1"`
}
type CancelInput struct {
	Reason  string `json:"reason" validate:"required,min=2,max=500"`
	Version int    `json:"version" validate:"required,min=1"`
}

const rxSQL = `SELECT (to_jsonb(p) - 'org_id' - 'created_by' - 'updated_by' - 'row_version') || jsonb_build_object('version',p.row_version,'items',
 coalesce((SELECT jsonb_agg((to_jsonb(i) - 'org_id' - 'created_by' - 'updated_by' - 'row_version' - 'prescription_id' - 'patient_id') || jsonb_build_object('version',i.row_version,'drug_name',coalesce(d.generic_name,i.drug_name_text),'strength',d.strength) ORDER BY i.created_at,i.id)
   FROM pharmacy.prescription_item i LEFT JOIN pharmacy.drug d ON d.id=i.drug_id WHERE i.prescription_id=p.id),'[]'::jsonb),
 'dispenses',coalesce((SELECT jsonb_agg(jsonb_build_object('id',x.id,'status',x.status,'dispensed_at',x.dispensed_at,'counselling_given',x.counselling_given,'reversal_reason',x.reversal_reason,
   'lines',(SELECT coalesce(jsonb_agg(jsonb_build_object('id',di.id,'prescription_item_id',di.prescription_item_id,'batch_id',di.batch_id,'quantity',di.quantity)),'[]'::jsonb) FROM pharmacy.dispense_item di WHERE di.dispense_id=x.id)) ORDER BY x.dispensed_at)
   FROM pharmacy.dispense x WHERE x.prescription_id=p.id),'[]'::jsonb)) FROM pharmacy.prescription p`

func (s Service) Prescribe(ctx context.Context, a identity.Actor, encounter string, in PrescriptionInput, key, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "pharmacy.prescribe"); e != nil {
		return nil, e
	}
	for _, it := range in.Items {
		if (it.DrugID == nil) == (it.DrugNameText == nil) {
			return nil, platform.Fail(422, "validation", "Each item needs exactly one of drug_id or drug_name_text")
		}
		if it.DurationDays == nil && it.FrequencyCode != "STAT" && it.FrequencyCode != "PRN" {
			return nil, platform.Fail(422, "validation", "duration_days is required unless the frequency is STAT or PRN")
		}
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		patient, _, e := platform.Encounter(ctx, tx, a.FacilityIDs, encounter)
		if e != nil {
			return nil, e
		}
		return platform.Idempotent(ctx, tx, sess, "rx.create:"+encounter, key, in, func() (Raw, error) {
			var id string
			e := tx.QueryRow(ctx, `INSERT INTO pharmacy.prescription(org_id,encounter_id,patient_id,prescription_no,prescriber_membership_id,notes)
 VALUES($1::uuid,$2::uuid,$3::uuid,'RX-'||to_char(org.local_date($1::uuid),'YYYY')||'-'||lpad(org.next_value($1::uuid,'prescription',to_char(org.local_date($1::uuid),'YYYY'))::text,6,'0'),$4::uuid,NULLIF($5,'')) RETURNING id::text`,
				sess.OrgID, encounter, patient, sess.MembershipID, strings.TrimSpace(in.Notes)).Scan(&id)
			if e != nil {
				return nil, e
			}
			for _, it := range in.Items {
				if _, e = tx.Exec(ctx, `INSERT INTO pharmacy.prescription_item(org_id,prescription_id,patient_id,drug_id,drug_name_text,dose_amount,dose_unit,route,frequency_code,duration_days,quantity_prescribed,instructions)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,$8,$9,$10,$11,NULLIF($12,''))`, sess.OrgID, id, patient, it.DrugID, it.DrugNameText, it.DoseAmount, it.DoseUnit, it.Route, it.FrequencyCode, it.DurationDays, it.QuantityPrescribed, strings.TrimSpace(it.Instructions)); e != nil {
					return nil, e
				}
			}
			if in.Sign {
				if _, e = tx.Exec(ctx, `UPDATE pharmacy.prescription SET status='active' WHERE id=$1::uuid`, id); e != nil {
					return nil, e
				}
			}
			if e = platform.Audit(ctx, tx, "prescription.create", "phi_access", "success", "prescription", id, patient, ""); e != nil {
				return nil, e
			}
			return platform.One(ctx, tx, rxSQL+` WHERE p.id=$1::uuid`, id)
		})
	})
}

func canRx(a identity.Actor) error {
	return platform.Need(a.Permissions, "pharmacy.read", "clinical.read")
}

// Prescriptions is the pharmacy queue / history, filtered by status, patient or visit.
func (s Service) Prescriptions(ctx context.Context, a identity.Actor, status, patient, encounter, after, request string, limit int) ([]Raw, error) {
	if e := canRx(a); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		out, e := platform.Many(ctx, tx, rxSQL+` WHERE p.patient_id IN (SELECT pt.id FROM patient.patient pt WHERE pt.registered_facility_id=ANY($1::uuid[]))
 AND ($2='' OR p.status=$2) AND ($3='' OR p.patient_id=NULLIF($3,'')::uuid) AND ($4='' OR p.encounter_id=NULLIF($4,'')::uuid) AND ($5='' OR p.id>NULLIF($5,'')::uuid) ORDER BY p.id LIMIT $6`,
			a.FacilityIDs, status, patient, encounter, after, limit)
		if e != nil {
			return nil, e
		}
		return out, platform.Audit(ctx, tx, "prescription.list", "phi_access", "success", "prescription", "", patient, "")
	})
}

func (s Service) Prescription(ctx context.Context, a identity.Actor, id, request string) (Raw, error) {
	if e := canRx(a); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		out, e := platform.One(ctx, tx, rxSQL+` WHERE p.id=$1::uuid AND p.patient_id IN (SELECT pt.id FROM patient.patient pt WHERE pt.registered_facility_id=ANY($2::uuid[]))`, id, a.FacilityIDs)
		if e != nil {
			return nil, e
		}
		var patient string
		tx.QueryRow(ctx, `SELECT patient_id::text FROM pharmacy.prescription WHERE id=$1::uuid`, id).Scan(&patient)
		return out, platform.Audit(ctx, tx, "prescription.view", "phi_access", "success", "prescription", id, patient, "")
	})
}

func (s Service) Sign(ctx context.Context, a identity.Actor, id string, in VersionInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "pharmacy.prescribe"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE pharmacy.prescription SET status='active' WHERE id=$1::uuid AND row_version=$2 AND status='draft'`, id, in.Version)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "pharmacy.prescription", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, rxSQL+` WHERE p.id=$1::uuid`, id)
	})
}

func (s Service) CancelRx(ctx context.Context, a identity.Actor, id string, in CancelInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "pharmacy.prescribe"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE pharmacy.prescription SET status='cancelled',cancel_reason=$3 WHERE id=$1::uuid AND row_version=$2`, id, in.Version, strings.TrimSpace(in.Reason))
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "pharmacy.prescription", id); e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, `UPDATE pharmacy.prescription_item SET status='cancelled' WHERE prescription_id=$1::uuid AND status='pending'`, id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, rxSQL+` WHERE p.id=$1::uuid`, id)
	})
}

// ---------------------------------------------------------------- dispensing

type DispenseLine struct {
	PrescriptionItemID string  `json:"prescription_item_id" validate:"required,uuid"`
	BatchID            *string `json:"batch_id" validate:"omitempty,uuid"`
	Quantity           float64 `json:"quantity" validate:"required,gt=0"`
}
type DispenseInput struct {
	FacilityID       string         `json:"facility_id" validate:"required,uuid"`
	CounsellingGiven bool           `json:"counselling_given"`
	Lines            []DispenseLine `json:"lines" validate:"required,min=1,max=30,dive"`
}

// Dispense issues drugs against a signed prescription. Without a batch_id the oldest-expiry
// stock (FEFO) at the prescription's facility is used. The Idempotency-Key header must be a UUID.
func (s Service) Dispense(ctx context.Context, a identity.Actor, rx string, in DispenseInput, key, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "pharmacy.dispense"); e != nil {
		return nil, e
	}
	k, e := uuid.Parse(key)
	if e != nil {
		return nil, platform.Fail(422, "idempotency_key_required", "Idempotency-Key must be a UUID")
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var existing string
		e := tx.QueryRow(ctx, `SELECT prescription_id::text FROM pharmacy.dispense WHERE idempotency_key=$1::uuid`, k.String()).Scan(&existing)
		if e == nil {
			if existing != rx {
				return nil, platform.Fail(409, "idempotency_conflict", "Key already used for another prescription")
			}
			return platform.One(ctx, tx, rxSQL+` WHERE p.id=$1::uuid`, rx)
		}
		if !errors.Is(e, pgx.ErrNoRows) {
			return nil, e
		}
		facility := in.FacilityID
		if !contains(a.FacilityIDs, facility) {
			return nil, platform.Forbidden()
		}
		var patient string
		if e = tx.QueryRow(ctx, `SELECT patient_id::text FROM pharmacy.prescription WHERE id=$1::uuid`, rx).Scan(&patient); e != nil {
			return nil, e
		}
		var did string
		if e = tx.QueryRow(ctx, `INSERT INTO pharmacy.dispense(org_id,facility_id,prescription_id,patient_id,dispensed_by_membership_id,counselling_given,idempotency_key)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,$7::uuid) RETURNING id::text`, sess.OrgID, facility, rx, patient, sess.MembershipID, in.CounsellingGiven, k.String()).Scan(&did); e != nil {
			return nil, e
		}
		for _, l := range in.Lines {
			batches := []struct {
				id  string
				qty float64
			}{}
			if l.BatchID != nil {
				batches = append(batches, struct {
					id  string
					qty float64
				}{*l.BatchID, l.Quantity})
			} else {
				var drug *string
				if e = tx.QueryRow(ctx, `SELECT drug_id::text FROM pharmacy.prescription_item WHERE id=$1::uuid AND prescription_id=$2::uuid`, l.PrescriptionItemID, rx).Scan(&drug); e != nil {
					return nil, e
				}
				if drug == nil {
					return nil, platform.Fail(422, "validation", "This item is not a stocked drug; it cannot be dispensed from stock")
				}
				rows, e := tx.Query(ctx, `SELECT id::text,quantity_on_hand::float8 FROM pharmacy.stock_batch WHERE facility_id=$1::uuid AND drug_id=$2::uuid AND status='active' AND quantity_on_hand>0 AND expiry_date>=org.local_date($3::uuid) ORDER BY expiry_date,received_at`, facility, *drug, sess.OrgID)
				if e != nil {
					return nil, e
				}
				need := l.Quantity
				for rows.Next() && need > 0 {
					var id string
					var have float64
					if e = rows.Scan(&id, &have); e != nil {
						rows.Close()
						return nil, e
					}
					take := have
					if take > need {
						take = need
					}
					batches = append(batches, struct {
						id  string
						qty float64
					}{id, take})
					need -= take
				}
				rows.Close()
				if need > 0 {
					return nil, platform.Fail(409, "insufficient_stock", "Not enough unexpired stock to dispense this quantity")
				}
			}
			for _, b := range batches {
				if _, e = tx.Exec(ctx, `INSERT INTO pharmacy.dispense_item(org_id,dispense_id,prescription_item_id,batch_id,quantity) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5)`, sess.OrgID, did, l.PrescriptionItemID, b.id, b.qty); e != nil {
					return nil, e
				}
			}
		}
		if e = platform.Audit(ctx, tx, "pharmacy.dispense", "phi_access", "success", "dispense", did, patient, ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, rxSQL+` WHERE p.id=$1::uuid`, rx)
	})
}

type ReverseInput struct {
	Reason string `json:"reason" validate:"required,min=5,max=500"`
}

// ReverseDispense returns a wrong or unopened dispense to stock.
func (s Service) ReverseDispense(ctx context.Context, a identity.Actor, id string, in ReverseInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "pharmacy.dispense"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		var rx string
		if e := tx.QueryRow(ctx, `SELECT d.prescription_id::text FROM pharmacy.dispense d WHERE d.id=$1::uuid AND d.facility_id=ANY($2::uuid[])`, id, a.FacilityIDs).Scan(&rx); e != nil {
			return nil, e
		}
		if _, e := tx.Exec(ctx, `SELECT pharmacy.reverse_dispense($1::uuid,$2)`, id, strings.TrimSpace(in.Reason)); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, rxSQL+` WHERE p.id=$1::uuid`, rx)
	})
}
