// Package diagnostics covers the test catalogue, laboratory/imaging requests, specimens and results.
package diagnostics

import (
	"context"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
)

type Service struct{ DB *pgxpool.Pool }
type Raw = platform.Raw

var (
	reqJ  = platform.JSON("r", true)
	specJ = platform.JSON("s", true, "collected_by", "received_by")
	resJ  = platform.JSON("x", true, "entered_by", "verified_by", "supersedes_id")
)

const testSQL = `SELECT (to_jsonb(t) - 'org_id' - 'created_by' - 'updated_by' - 'row_version') || jsonb_build_object('version',t.row_version,'components',
 coalesce((SELECT jsonb_agg((to_jsonb(c) - 'org_id' - 'test_id') ORDER BY c.display_order) FROM diag.test_component c WHERE c.test_id=t.id AND c.status='active'),'[]'::jsonb)) FROM diag.test_catalog t`

// ---------------------------------------------------------------- catalogue

type ComponentInput struct {
	Code          string   `json:"code" validate:"required,min=1,max=30"`
	Name          string   `json:"name" validate:"required,min=1,max=120"`
	ValueType     string   `json:"value_type" validate:"required,oneof=numeric text coded attachment"`
	Unit          string   `json:"unit" validate:"max=30"`
	DecimalPlaces *int     `json:"decimal_places" validate:"omitempty,min=0,max=6"`
	AllowedValues []string `json:"allowed_values" validate:"max=50,dive,min=1,max=60"`
	DisplayOrder  int      `json:"display_order" validate:"omitempty,min=1,max=999"`
}
type TestInput struct {
	Code            string           `json:"code" validate:"required,max=30"`
	Name            string           `json:"name" validate:"required,min=2,max=160"`
	Category        string           `json:"category" validate:"required,oneof=laboratory imaging other_diagnostic"`
	SpecimenType    *string          `json:"specimen_type" validate:"omitempty,oneof=blood serum plasma urine stool sputum swab csf tissue other"`
	TurnaroundHours *int             `json:"turnaround_hours" validate:"omitempty,min=0,max=720"`
	Components      []ComponentInput `json:"components" validate:"required,min=1,max=40,dive"`
}
type RangeInput struct {
	Sex          *string  `json:"sex" validate:"omitempty,oneof=female male"`
	AgeMinDays   int      `json:"age_min_days" validate:"min=0"`
	AgeMaxDays   *int     `json:"age_max_days" validate:"omitempty,min=0"`
	Low          *float64 `json:"low"`
	High         *float64 `json:"high"`
	CriticalLow  *float64 `json:"critical_low"`
	CriticalHigh *float64 `json:"critical_high"`
}

func (s Service) Tests(ctx context.Context, a identity.Actor, category, request string) ([]Raw, error) {
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, testSQL+` WHERE t.status='active' AND ($1='' OR t.category=$1) ORDER BY t.name`, category)
	})
}

func (s Service) CreateTest(ctx context.Context, a identity.Actor, in TestInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "diag.catalog.manage"); e != nil {
		return nil, e
	}
	in.Code = strings.ToUpper(strings.TrimSpace(in.Code))
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO diag.test_catalog(org_id,code,name,category,specimen_type,turnaround_hours) VALUES($1::uuid,$2,$3,$4,$5,$6) RETURNING id::text`,
			sess.OrgID, in.Code, strings.TrimSpace(in.Name), in.Category, in.SpecimenType, in.TurnaroundHours).Scan(&id)
		if e != nil {
			return nil, e
		}
		for i, c := range in.Components {
			order := c.DisplayOrder
			if order == 0 {
				order = i + 1
			}
			var allowed any
			if len(c.AllowedValues) > 0 {
				allowed = c.AllowedValues
			}
			if _, e = tx.Exec(ctx, `INSERT INTO diag.test_component(org_id,test_id,code,name,value_type,unit,decimal_places,allowed_values,display_order) VALUES($1::uuid,$2::uuid,$3,$4,$5,NULLIF($6,''),$7,$8,$9)`,
				sess.OrgID, id, strings.ToUpper(strings.TrimSpace(c.Code)), strings.TrimSpace(c.Name), c.ValueType, c.Unit, c.DecimalPlaces, allowed, order); e != nil {
				return nil, e
			}
		}
		return platform.One(ctx, tx, testSQL+` WHERE t.id=$1::uuid`, id)
	})
}

func (s Service) AddRange(ctx context.Context, a identity.Actor, component string, in RangeInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "diag.catalog.manage"); e != nil {
		return nil, e
	}
	max := 54750
	if in.AgeMaxDays != nil {
		max = *in.AgeMaxDays
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		return platform.One(ctx, tx, `WITH r AS (INSERT INTO diag.reference_range(org_id,component_id,sex,age_min_days,age_max_days,low,high,critical_low,critical_high)
 VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8,$9) RETURNING *) SELECT to_jsonb(r) - 'org_id' FROM r`, sess.OrgID, component, in.Sex, in.AgeMinDays, max, in.Low, in.High, in.CriticalLow, in.CriticalHigh)
	})
}

// ---------------------------------------------------------------- requests

type RequestInput struct {
	Priority           string   `json:"priority" validate:"omitempty,oneof=routine urgent stat"`
	ClinicalIndication string   `json:"clinical_indication" validate:"required,min=3,max=1000"`
	TestIDs            []string `json:"test_ids" validate:"required,min=1,max=30,dive,uuid"`
}
type CancelInput struct {
	Reason  string `json:"reason" validate:"required,min=2,max=500"`
	Version int    `json:"version" validate:"required,min=1"`
}

const reqDetail = `SELECT ` + `(to_jsonb(r) - 'org_id' - 'created_by' - 'updated_by' - 'row_version')` + ` || jsonb_build_object('version',r.row_version,'items',
 coalesce((SELECT jsonb_agg(jsonb_build_object('id',i.id,'test_id',i.test_id,'test_code',t.code,'test_name',t.name,'category',t.category,'status',i.status,'cancel_reason',i.cancel_reason,'version',i.row_version,
   'specimens',coalesce((SELECT jsonb_agg(jsonb_build_object('id',sp.id,'barcode',sp.barcode,'specimen_type',sp.specimen_type,'status',sp.status,'collected_at',sp.collected_at,'received_at',sp.received_at,'rejection_reason',sp.rejection_reason,'version',sp.row_version) ORDER BY sp.collected_at) FROM diag.specimen sp WHERE sp.request_item_id=i.id),'[]'::jsonb),
   'results',coalesce((SELECT jsonb_agg(jsonb_build_object('id',x.id,'component_id',x.component_id,'component_code',c.code,'component_name',c.name,'value_numeric',x.value_numeric,'value_text',x.value_text,'value_coded',x.value_coded,'unit',x.unit,'ref_low',x.ref_low,'ref_high',x.ref_high,'abnormal_flag',x.abnormal_flag,'is_critical',x.is_critical,'status',x.status,'entered_at',x.entered_at,'verified_at',x.verified_at,'version',x.row_version) ORDER BY c.display_order) FROM diag.result x JOIN diag.test_component c ON c.id=x.component_id WHERE x.request_item_id=i.id AND x.status<>'entered_in_error'),'[]'::jsonb)
  ) ORDER BY t.name) FROM diag.request_item i JOIN diag.test_catalog t ON t.id=i.test_id WHERE i.request_id=r.id),'[]'::jsonb)) FROM diag.request r`

// CreateRequest orders one or more tests for a visit.
func (s Service) CreateRequest(ctx context.Context, a identity.Actor, encounter string, in RequestInput, key, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "diag.request.create"); e != nil {
		return nil, e
	}
	if in.Priority == "" {
		in.Priority = "routine"
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		patient, facility, e := platform.Encounter(ctx, tx, a.FacilityIDs, encounter)
		if e != nil {
			return nil, e
		}
		return platform.Idempotent(ctx, tx, sess, "diag.request:"+encounter, key, in, func() (Raw, error) {
			var id string
			e := tx.QueryRow(ctx, `INSERT INTO diag.request(org_id,facility_id,encounter_id,patient_id,request_no,ordered_by_membership_id,priority,clinical_indication)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,'DX-'||to_char(org.local_date($1::uuid),'YYYY')||'-'||lpad(org.next_value($1::uuid,'diag_request',to_char(org.local_date($1::uuid),'YYYY'))::text,6,'0'),$5::uuid,$6,$7) RETURNING id::text`,
				sess.OrgID, facility, encounter, patient, sess.MembershipID, in.Priority, strings.TrimSpace(in.ClinicalIndication)).Scan(&id)
			if e != nil {
				return nil, e
			}
			seen := map[string]bool{}
			for _, t := range in.TestIDs {
				if seen[t] {
					continue
				}
				seen[t] = true
				if _, e = tx.Exec(ctx, `INSERT INTO diag.request_item(org_id,request_id,patient_id,test_id) VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid)`, sess.OrgID, id, patient, t); e != nil {
					return nil, e
				}
			}
			if e = platform.Audit(ctx, tx, "diagnostic.request.create", "phi_access", "success", "diag_request", id, patient, ""); e != nil {
				return nil, e
			}
			return platform.One(ctx, tx, reqDetail+` WHERE r.id=$1::uuid`, id)
		})
	})
}

func canRead(a identity.Actor) error {
	return platform.Need(a.Permissions, "diag.request.read", "clinical.read")
}

// Requests is the worklist: filter by status, patient or visit.
func (s Service) Requests(ctx context.Context, a identity.Actor, status, patient, encounter, after, request string, limit int) ([]Raw, error) {
	if e := canRead(a); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		out, e := platform.Many(ctx, tx, reqDetail+` WHERE r.facility_id=ANY($1::uuid[]) AND ($2='' OR r.status=$2) AND ($3='' OR r.patient_id=NULLIF($3,'')::uuid)
 AND ($4='' OR r.encounter_id=NULLIF($4,'')::uuid) AND ($5='' OR r.id>NULLIF($5,'')::uuid) ORDER BY r.id LIMIT $6`, a.FacilityIDs, status, patient, encounter, after, limit)
		if e != nil {
			return nil, e
		}
		return out, platform.Audit(ctx, tx, "diagnostic.request.list", "phi_access", "success", "diag_request", "", patient, "")
	})
}

func (s Service) Request(ctx context.Context, a identity.Actor, id, request string) (Raw, error) {
	if e := canRead(a); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		out, e := platform.One(ctx, tx, reqDetail+` WHERE r.id=$1::uuid AND r.facility_id=ANY($2::uuid[])`, id, a.FacilityIDs)
		if e != nil {
			return nil, e
		}
		var patient string
		tx.QueryRow(ctx, `SELECT patient_id::text FROM diag.request WHERE id=$1::uuid`, id).Scan(&patient)
		return out, platform.Audit(ctx, tx, "diagnostic.request.view", "phi_access", "success", "diag_request", id, patient, "")
	})
}

func (s Service) CancelRequest(ctx context.Context, a identity.Actor, id string, in CancelInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "diag.request.create"); e != nil {
		return nil, e
	}
	reason := strings.TrimSpace(in.Reason)
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE diag.request SET status='cancelled',cancel_reason=$3 WHERE id=$1::uuid AND row_version=$2 AND facility_id=ANY($4::uuid[])`, id, in.Version, reason, a.FacilityIDs)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "diag.request", id); e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, `UPDATE diag.request_item SET status='cancelled',cancel_reason=$2 WHERE request_id=$1::uuid AND status='requested'`, id, reason); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, reqDetail+` WHERE r.id=$1::uuid`, id)
	})
}

// ---------------------------------------------------------------- specimens

type SpecimenInput struct {
	SpecimenType string `json:"specimen_type" validate:"omitempty,oneof=blood serum plasma urine stool sputum swab csf tissue other"`
}
type RejectInput struct {
	Reason  string `json:"reason" validate:"required,oneof=haemolysed clotted insufficient_volume unlabelled mislabelled wrong_container delayed_transport other"`
	Version int    `json:"version" validate:"required,min=1"`
}
type VersionInput struct {
	Version int `json:"version" validate:"required,min=1"`
}

func (s Service) itemRef(ctx context.Context, tx pgx.Tx, a identity.Actor, item string) (request, patient string, err error) {
	err = tx.QueryRow(ctx, `SELECT r.id::text,r.patient_id::text FROM diag.request_item i JOIN diag.request r ON r.id=i.request_id WHERE i.id=$1::uuid AND r.facility_id=ANY($2::uuid[])`, item, a.FacilityIDs).Scan(&request, &patient)
	return
}

// CollectSpecimen records the collection of a specimen for a requested test and returns the whole request.
func (s Service) CollectSpecimen(ctx context.Context, a identity.Actor, item string, in SpecimenInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "diag.specimen.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		req, patient, e := s.itemRef(ctx, tx, a, item)
		if e != nil {
			return nil, e
		}
		var kind *string
		var cat string
		if e = tx.QueryRow(ctx, `SELECT t.specimen_type,t.category FROM diag.request_item i JOIN diag.test_catalog t ON t.id=i.test_id WHERE i.id=$1::uuid`, item).Scan(&kind, &cat); e != nil {
			return nil, e
		}
		typ := in.SpecimenType
		if typ == "" && kind != nil {
			typ = *kind
		}
		if typ == "" {
			return nil, platform.Fail(422, "validation", "specimen_type is required")
		}
		if _, e = tx.Exec(ctx, `UPDATE diag.request_item SET status='specimen_collected' WHERE id=$1::uuid AND status='requested'`, item); e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, `INSERT INTO diag.specimen(org_id,request_item_id,patient_id,barcode,specimen_type)
 VALUES($1::uuid,$2::uuid,$3::uuid,'SPC-'||to_char(org.local_date($1::uuid),'YYMMDD')||'-'||lpad(org.next_value($1::uuid,'specimen',org.local_date($1::uuid)::text)::text,5,'0'),$4)`, sess.OrgID, item, patient, typ); e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, `UPDATE diag.request SET status='in_progress' WHERE id=$1::uuid AND status='requested'`, req); e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "diagnostic.specimen.collect", "phi_access", "success", "diag_request", req, patient, ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, reqDetail+` WHERE r.id=$1::uuid`, req)
	})
}

func (s Service) specimenAction(ctx context.Context, a identity.Actor, id, request, sql string, args ...any) (Raw, error) {
	if e := platform.Need(a.Permissions, "diag.specimen.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, sql, append([]any{id}, args...)...)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "diag.specimen", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+specJ+` FROM diag.specimen s WHERE id=$1::uuid`, id)
	})
}

func (s Service) ReceiveSpecimen(ctx context.Context, a identity.Actor, id string, in VersionInput, request string) (Raw, error) {
	return s.specimenAction(ctx, a, id, request, `UPDATE diag.specimen SET status='received',received_at=now(),received_by=sec.current_user_id() WHERE id=$1::uuid AND row_version=$2 AND status='collected'`, in.Version)
}

// RejectSpecimen rejects a specimen and its test item so the specimen can be collected again.
func (s Service) RejectSpecimen(ctx context.Context, a identity.Actor, id string, in RejectInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "diag.specimen.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE diag.specimen SET status='rejected',rejection_reason=$3 WHERE id=$1::uuid AND row_version=$2 AND status IN ('collected','received')`, id, in.Version, in.Reason)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "diag.specimen", id); e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, `UPDATE diag.request_item SET status='rejected',cancel_reason='specimen '||$2 WHERE id=(SELECT request_item_id FROM diag.specimen WHERE id=$1::uuid) AND status='specimen_collected'`, id, in.Reason); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+specJ+` FROM diag.specimen s WHERE id=$1::uuid`, id)
	})
}

// Recollect puts a rejected test back to "requested" so a new specimen can be taken.
func (s Service) Recollect(ctx context.Context, a identity.Actor, item string, in VersionInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "diag.specimen.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		req, _, e := s.itemRef(ctx, tx, a, item)
		if e != nil {
			return nil, e
		}
		tag, e := tx.Exec(ctx, `UPDATE diag.request_item SET status='requested',cancel_reason=NULL WHERE id=$1::uuid AND row_version=$2 AND status='rejected'`, item, in.Version)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "diag.request_item", item); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, reqDetail+` WHERE r.id=$1::uuid`, req)
	})
}

// ---------------------------------------------------------------- results

type ResultValue struct {
	ComponentID  string   `json:"component_id" validate:"required,uuid"`
	ValueNumeric *float64 `json:"value_numeric"`
	ValueText    *string  `json:"value_text" validate:"omitempty,max=4000"`
	ValueCoded   *string  `json:"value_coded" validate:"omitempty,max=100"`
}
type ResultsInput struct {
	Results []ResultValue `json:"results" validate:"required,min=1,max=60,dive"`
}

// EnterResults records preliminary results for the components of one test item.
func (s Service) EnterResults(ctx context.Context, a identity.Actor, item string, in ResultsInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "diag.result.enter"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		req, patient, e := s.itemRef(ctx, tx, a, item)
		if e != nil {
			return nil, e
		}
		var sex *string
		var ageDays int
		if e = tx.QueryRow(ctx, `SELECT CASE WHEN sex IN ('female','male') THEN sex END,(current_date-date_of_birth) FROM patient.patient WHERE id=$1::uuid`, patient).Scan(&sex, &ageDays); e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, `UPDATE diag.request_item SET status='in_progress' WHERE id=$1::uuid AND status IN ('requested','specimen_collected')`, item); e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, `UPDATE diag.request SET status='in_progress' WHERE id=$1::uuid AND status='requested'`, req); e != nil {
			return nil, e
		}
		var encounterID, orderedBy string
		if e = tx.QueryRow(ctx, `SELECT encounter_id::text,ordered_by_membership_id::text FROM diag.request WHERE id=$1::uuid`, req).Scan(&encounterID, &orderedBy); e != nil {
			return nil, e
		}
		for _, v := range in.Results {
			var vtype string
			var allowed []string
			if e = tx.QueryRow(ctx, `SELECT c.value_type,coalesce(c.allowed_values,'{}') FROM diag.test_component c JOIN diag.request_item i ON i.test_id=c.test_id WHERE i.id=$1::uuid AND c.id=$2::uuid AND c.status='active'`, item, v.ComponentID).Scan(&vtype, &allowed); e != nil {
				return nil, platform.Fail(422, "validation", "component does not belong to this test")
			}
			switch {
			case vtype == "numeric" && v.ValueNumeric == nil, vtype == "text" && v.ValueText == nil, vtype == "coded" && v.ValueCoded == nil, vtype == "attachment":
				return nil, platform.Fail(422, "validation", "a "+vtype+" value is required for this component")
			case vtype == "coded" && !slices.Contains(allowed, *v.ValueCoded):
				return nil, platform.Fail(422, "validation", "value is not one of the allowed values")
			}
			var resID, flag string
			var isCrit bool
			if e = tx.QueryRow(ctx, `INSERT INTO diag.result(org_id,request_item_id,patient_id,component_id,value_numeric,value_text,value_coded,unit,ref_low,ref_high,abnormal_flag,is_critical)
 SELECT $1::uuid,$2::uuid,$3::uuid,c.id,$4::numeric,$5,$6,c.unit,rr.low,rr.high,
  CASE WHEN $4::numeric < rr.critical_low THEN 'LL' WHEN $4::numeric > rr.critical_high THEN 'HH' END,
  coalesce($4::numeric < rr.critical_low OR $4::numeric > rr.critical_high,false)
 FROM diag.test_component c LEFT JOIN LATERAL (SELECT * FROM diag.reference_range r WHERE r.component_id=c.id AND (r.sex IS NULL OR r.sex=$7)
   AND $8::int BETWEEN r.age_min_days AND r.age_max_days ORDER BY (r.sex IS NULL) LIMIT 1) rr ON true WHERE c.id=$9::uuid
 RETURNING id::text, is_critical, coalesce(abnormal_flag,'')`,
				sess.OrgID, item, patient, v.ValueNumeric, v.ValueText, v.ValueCoded, sex, ageDays, v.ComponentID).Scan(&resID, &isCrit, &flag); e != nil {
				return nil, e
			}
			if isCrit {
				if _, e = tx.Exec(ctx, `INSERT INTO comms.outbox(org_id,event_type,aggregate_type,aggregate_id,dedupe_key,payload)
 VALUES($1::uuid,'diagnostic.critical_result','diag_result',$2::uuid,'critical_result:'||$3::text,
  jsonb_build_object('result_id',$3::text,'request_id',$4::text,'encounter_id',$5::text,'abnormal_flag',$6::text,'stage','preliminary','ordered_by_membership_id',$7::text))
 ON CONFLICT DO NOTHING`, sess.OrgID, resID, resID, req, encounterID, flag, orderedBy); e != nil {
					return nil, e
				}
				if e = platform.Audit(ctx, tx, "diagnostic.result.critical_alert", "phi_access", "success", "diag_result", resID, patient, "critical_"+flag); e != nil {
					return nil, e
				}
			}
		}
		if e = platform.Audit(ctx, tx, "diagnostic.result.enter", "phi_access", "success", "diag_request", req, patient, ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, reqDetail+` WHERE r.id=$1::uuid`, req)
	})
}

// VerifyResult finalises a preliminary result; the database requires a second person unless the organisation allows self-verification.
func (s Service) VerifyResult(ctx context.Context, a identity.Actor, id string, in VersionInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "diag.result.verify"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE diag.result SET status='final' WHERE id=$1::uuid AND row_version=$2 AND status='preliminary'`, id, in.Version)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "diag.result", id); e != nil {
			return nil, e
		}
		var patient, reqID, encID, orderedBy, flag string
		var isCrit bool
		if e = tx.QueryRow(ctx, `SELECT x.patient_id::text,r.id::text,r.encounter_id::text,r.ordered_by_membership_id::text,x.is_critical,coalesce(x.abnormal_flag,'')
 FROM diag.result x JOIN diag.request_item i ON i.id=x.request_item_id JOIN diag.request r ON r.id=i.request_id WHERE x.id=$1::uuid`, id).
			Scan(&patient, &reqID, &encID, &orderedBy, &isCrit, &flag); e != nil {
			return nil, e
		}
		if isCrit {
			if _, e = tx.Exec(ctx, `INSERT INTO comms.outbox(org_id,event_type,aggregate_type,aggregate_id,dedupe_key,payload)
 VALUES($1::uuid,'diagnostic.critical_result_verified','diag_result',$2::uuid,'critical_result_verified:'||$3::text,
  jsonb_build_object('result_id',$3::text,'request_id',$4::text,'encounter_id',$5::text,'abnormal_flag',$6::text,'stage','final','ordered_by_membership_id',$7::text))
 ON CONFLICT DO NOTHING`, sess.OrgID, id, id, reqID, encID, flag, orderedBy); e != nil {
				return nil, e
			}
			if e = platform.Audit(ctx, tx, "diagnostic.result.critical_verified", "phi_access", "success", "diag_result", id, patient, "critical_"+flag); e != nil {
				return nil, e
			}
		}
		if e = platform.Audit(ctx, tx, "diagnostic.result.verify", "phi_access", "success", "diag_result", id, patient, ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+resJ+` FROM diag.result x WHERE id=$1::uuid`, id)
	})
}

type VoidInput struct {
	Reason  string `json:"reason" validate:"required,min=2,max=500"`
	Version int    `json:"version" validate:"required,min=1"`
}

func (s Service) VoidResult(ctx context.Context, a identity.Actor, id string, in VoidInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "diag.result.enter", "diag.result.verify"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE diag.result SET status='entered_in_error',error_reason=$3 WHERE id=$1::uuid AND row_version=$2`, id, in.Version, strings.TrimSpace(in.Reason))
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "diag.result", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+resJ+` FROM diag.result x WHERE id=$1::uuid`, id)
	})
}
