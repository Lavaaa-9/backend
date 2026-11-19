package patients

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"strings"

	"github.com/jackc/pgx/v5"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
)

type Raw = platform.Raw

var (
	identJ   = platform.JSON("i", true, "value_ciphertext", "value_bidx", "key_ref")
	contactJ = platform.JSON("c", true)
	nokJ     = platform.JSON("n", true)
	consentJ = platform.JSON("c", true)
	dupJ     = platform.JSON("d", true)
)

func ensurePatientInScope(ctx context.Context, tx pgx.Tx, facilities []string, patientID string) error {
	var ok bool
	if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patient.patient WHERE id=$1::uuid AND registered_facility_id=ANY($2::uuid[]))`, patientID, facilities).Scan(&ok); e != nil {
		return e
	}
	if !ok {
		return platform.NotFound()
	}
	return nil
}

// NormalizeIdentifier canonicalizes sensitive identifiers for blind indexing.
func NormalizeIdentifier(v string) string {
	v = strings.ToUpper(strings.TrimSpace(v))
	return strings.NewReplacer(" ", "", "-", "", "/", "").Replace(v)
}

func last4Alnum(v string) *string {
	var b strings.Builder
	for _, r := range v {
		if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if s == "" {
		return nil
	}
	if len(s) > 4 {
		s = s[len(s)-4:]
	}
	return &s
}

func BlindIndex(indexKey []byte, scope, normalized string) []byte {
	mac := hmac.New(sha256.New, indexKey)
	mac.Write([]byte(scope))
	mac.Write([]byte{0})
	mac.Write([]byte(normalized))
	return mac.Sum(nil)
}

func identifierAAD(orgID, patientID, idType string) []byte {
	return []byte("patient_identifier:" + orgID + ":" + patientID + ":" + idType)
}

// ---------------------------------------------------------------- identifiers

type IdentifierInput struct {
	IDType         string `json:"id_type" validate:"required,oneof=nida_nin nhif_member passport driving_licence voter_id birth_certificate zanzibar_id legacy_powerhms other"`
	Value          string `json:"value" validate:"required,min=2,max=100"`
	IssuingCountry string `json:"issuing_country" validate:"omitempty,len=2,uppercase"`
}

type IdentifierStatusInput struct {
	Status  string `json:"status" validate:"required,oneof=active inactive entered_in_error"`
	Version int    `json:"version" validate:"required,min=1"`
}

type RevealInput struct {
	Reason string `json:"reason" validate:"required,min=5,max=500"`
}

func (s Service) requireCrypto() error {
	if s.Keys == nil || len(s.IndexKey) != 32 {
		return platform.Fail(503, "encryption_unavailable", "Identifier encryption keys are not configured on this server")
	}
	return nil
}

func (s Service) AddIdentifier(ctx context.Context, a identity.Actor, patientID string, in IdentifierInput, key, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "patient.create", "patient.update"); e != nil {
		return nil, e
	}
	if e := s.requireCrypto(); e != nil {
		return nil, e
	}
	raw := strings.TrimSpace(in.Value)
	norm := NormalizeIdentifier(raw)
	if len(norm) < 2 {
		return nil, platform.Fail(422, "validation", "Identifier value is too short")
	}
	country := in.IssuingCountry
	if country == "" {
		country = "TZ"
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		if e := ensurePatientInScope(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		return platform.Idempotent(ctx, tx, sess, "patient.identifier.add:"+patientID, key, map[string]string{"id_type": in.IDType, "norm": norm, "country": country}, func() (Raw, error) {
			ct, e := s.Keys.Seal([]byte(raw), identifierAAD(sess.OrgID, patientID, in.IDType))
			if e != nil {
				return nil, e
			}
			bidx := BlindIndex(s.IndexKey, "patient_identifier:"+in.IDType, norm)
			l4 := last4Alnum(norm)
			var id string
			e = tx.QueryRow(ctx, `INSERT INTO patient.identifier(org_id,patient_id,id_type,value_ciphertext,value_bidx,value_last4,key_ref,issuing_country)
 VALUES($1::uuid,$2::uuid,$3,$4,$5,$6,$7,$8) RETURNING id::text`,
				sess.OrgID, patientID, in.IDType, ct, bidx, l4, s.Keys.ActiveKeyID(), country).Scan(&id)
			if e != nil {
				return nil, e
			}
			if e = platform.Audit(ctx, tx, "patient.identifier.create", "phi_access", "success", "patient_identifier", id, patientID, ""); e != nil {
				return nil, e
			}
			return platform.One(ctx, tx, `SELECT `+identJ+` FROM patient.identifier i WHERE id=$1::uuid`, id)
		})
	})
}

func (s Service) ListIdentifiers(ctx context.Context, a identity.Actor, patientID, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "patient.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		if e := ensurePatientInScope(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		out, e := platform.Many(ctx, tx, `SELECT `+identJ+` FROM patient.identifier i WHERE i.patient_id=$1::uuid ORDER BY i.created_at DESC`, patientID)
		if e != nil {
			return nil, e
		}
		return out, platform.Audit(ctx, tx, "patient.identifier.list", "phi_access", "success", "patient", patientID, patientID, "")
	})
}

func (s Service) RevealIdentifier(ctx context.Context, a identity.Actor, patientID, identID string, in RevealInput, request string) (map[string]any, error) {
	if e := platform.Need(a.Permissions, "patient.identifier.reveal"); e != nil {
		return nil, e
	}
	if e := s.requireCrypto(); e != nil {
		return nil, e
	}
	reason, e := platform.Reason(in.Reason, 5)
	if e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (map[string]any, error) {
		if e := ensurePatientInScope(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		var idType, status string
		var ct []byte
		e := tx.QueryRow(ctx, `SELECT id_type,status,value_ciphertext FROM patient.identifier WHERE id=$1::uuid AND patient_id=$2::uuid`, identID, patientID).
			Scan(&idType, &status, &ct)
		if e != nil {
			return nil, e
		}
		pt, e := s.Keys.Open(ct, identifierAAD(sess.OrgID, patientID, idType))
		if e != nil {
			return nil, platform.Fail(500, "decrypt_failed", "Stored identifier could not be decrypted")
		}
		if e = platform.Audit(ctx, tx, "patient.identifier.reveal", "phi_access", "success", "patient_identifier", identID, patientID, reason); e != nil {
			return nil, e
		}
		return map[string]any{"id": identID, "patient_id": patientID, "id_type": idType, "status": status, "value": string(pt)}, nil
	})
}

func (s Service) SetIdentifierStatus(ctx context.Context, a identity.Actor, patientID, identID string, in IdentifierStatusInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "patient.update"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		if e := ensurePatientInScope(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		tag, e := tx.Exec(ctx, `UPDATE patient.identifier SET status=$3 WHERE id=$1::uuid AND patient_id=$2::uuid AND row_version=$4`, identID, patientID, in.Status, in.Version)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "patient.identifier", identID); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+identJ+` FROM patient.identifier i WHERE id=$1::uuid`, identID)
	})
}

func (s Service) SearchIdentifier(ctx context.Context, a identity.Actor, in IdentifierInput, request string) ([]string, error) {
	if e := platform.Need(a.Permissions, "patient.read"); e != nil {
		return nil, e
	}
	if e := s.requireCrypto(); e != nil {
		return nil, e
	}
	norm := NormalizeIdentifier(in.Value)
	if len(norm) < 2 {
		return nil, platform.Fail(422, "validation", "Identifier value is too short")
	}
	bidx := BlindIndex(s.IndexKey, "patient_identifier:"+in.IDType, norm)
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]string, error) {
		rows, e := tx.Query(ctx, `SELECT DISTINCT i.patient_id::text FROM patient.identifier i
 JOIN patient.patient p ON p.id=i.patient_id
 WHERE i.id_type=$1 AND i.value_bidx=$2 AND i.status='active' AND p.registered_facility_id=ANY($3::uuid[])
 LIMIT 20`, in.IDType, bidx, a.FacilityIDs)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out := []string{}
		for rows.Next() {
			var pid string
			if e = rows.Scan(&pid); e != nil {
				return nil, e
			}
			out = append(out, pid)
		}
		if e = rows.Err(); e != nil {
			return nil, e
		}
		rows.Close()
		if e = platform.Audit(ctx, tx, "patient.identifier.search", "phi_access", "success", "patient_identifier", "", "", in.IDType); e != nil {
			return nil, e
		}
		return out, nil
	})
}

// ---------------------------------------------------------------- contact points

type ContactInput struct {
	System    string `json:"system" validate:"required,oneof=phone whatsapp email"`
	Value     string `json:"value" validate:"required,max=254"`
	IsPrimary bool   `json:"is_primary"`
}

type ContactUpdate struct {
	Value     *string `json:"value" validate:"omitempty,max=254"`
	IsPrimary *bool   `json:"is_primary"`
	Status    *string `json:"status" validate:"omitempty,oneof=active inactive"`
	Version   int     `json:"version" validate:"required,min=1"`
}

func normalizeContact(system, value string) (string, error) {
	if system == "email" {
		v := strings.ToLower(strings.TrimSpace(value))
		if v == "" || !strings.Contains(v, "@") {
			return "", platform.Fail(422, "validation", "Invalid email address")
		}
		return v, nil
	}
	v, e := phone(value)
	if e != nil || v == "" {
		return "", platform.Fail(422, "validation", "Phone must be a valid international number such as +255712345678")
	}
	return v, nil
}

func (s Service) ListContacts(ctx context.Context, a identity.Actor, patientID, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "patient.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		if e := ensurePatientInScope(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		return platform.Many(ctx, tx, `SELECT `+contactJ+` FROM patient.contact_point c WHERE c.patient_id=$1::uuid ORDER BY c.is_primary DESC, c.created_at`, patientID)
	})
}

func (s Service) AddContact(ctx context.Context, a identity.Actor, patientID string, in ContactInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "patient.create", "patient.update"); e != nil {
		return nil, e
	}
	val, e := normalizeContact(in.System, in.Value)
	if e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		if e := ensurePatientInScope(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		if in.IsPrimary {
			if _, e := tx.Exec(ctx, `UPDATE patient.contact_point SET is_primary=false WHERE patient_id=$1::uuid AND system=$2 AND is_primary AND status='active'`, patientID, in.System); e != nil {
				return nil, e
			}
		}
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO patient.contact_point(org_id,patient_id,system,value,is_primary) VALUES($1::uuid,$2::uuid,$3,$4,$5) RETURNING id::text`,
			sess.OrgID, patientID, in.System, val, in.IsPrimary).Scan(&id)
		if e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+contactJ+` FROM patient.contact_point c WHERE id=$1::uuid`, id)
	})
}

func (s Service) UpdateContact(ctx context.Context, a identity.Actor, patientID, contactID string, in ContactUpdate, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "patient.update"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		if e := ensurePatientInScope(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		var sys, curVal, curStatus string
		var curPrimary bool
		e := tx.QueryRow(ctx, `SELECT system,value,is_primary,status FROM patient.contact_point WHERE id=$1::uuid AND patient_id=$2::uuid FOR UPDATE`, contactID, patientID).
			Scan(&sys, &curVal, &curPrimary, &curStatus)
		if e != nil {
			return nil, e
		}
		if in.Value != nil {
			v, e := normalizeContact(sys, *in.Value)
			if e != nil {
				return nil, e
			}
			curVal = v
		}
		if in.Status != nil {
			curStatus = *in.Status
		}
		if in.IsPrimary != nil {
			curPrimary = *in.IsPrimary
		}
		if curStatus == "inactive" {
			curPrimary = false
		}
		if curPrimary {
			if _, e := tx.Exec(ctx, `UPDATE patient.contact_point SET is_primary=false WHERE patient_id=$1::uuid AND system=$2 AND id<>$3::uuid AND is_primary AND status='active'`, patientID, sys, contactID); e != nil {
				return nil, e
			}
		}
		tag, e := tx.Exec(ctx, `UPDATE patient.contact_point SET value=$3,is_primary=$4,status=$5 WHERE id=$1::uuid AND patient_id=$2::uuid AND row_version=$6`,
			contactID, patientID, curVal, curPrimary, curStatus, in.Version)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "patient.contact_point", contactID); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+contactJ+` FROM patient.contact_point c WHERE id=$1::uuid`, contactID)
	})
}

// ---------------------------------------------------------------- next of kin

type NextOfKinInput struct {
	FullName           string `json:"full_name" validate:"required,min=2,max=150"`
	Relationship       string `json:"relationship" validate:"required,oneof=spouse parent child sibling guardian relative friend employer other"`
	Phone              string `json:"phone" validate:"required,max=40"`
	AltPhone           string `json:"alt_phone" validate:"max=40"`
	Address            string `json:"address" validate:"max=500"`
	IsEmergencyContact *bool  `json:"is_emergency_contact"`
	Priority           int    `json:"priority" validate:"omitempty,min=1,max=5"`
}

type NextOfKinUpdate struct {
	FullName           *string `json:"full_name" validate:"omitempty,min=2,max=150"`
	Relationship       *string `json:"relationship" validate:"omitempty,oneof=spouse parent child sibling guardian relative friend employer other"`
	Phone              *string `json:"phone" validate:"omitempty,max=40"`
	AltPhone           *string `json:"alt_phone" validate:"omitempty,max=40"`
	Address            *string `json:"address" validate:"omitempty,max=500"`
	IsEmergencyContact *bool   `json:"is_emergency_contact"`
	Priority           *int    `json:"priority" validate:"omitempty,min=1,max=5"`
	Status             *string `json:"status" validate:"omitempty,oneof=active inactive"`
	Version            int     `json:"version" validate:"required,min=1"`
}

func (s Service) ListNextOfKin(ctx context.Context, a identity.Actor, patientID, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "patient.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		if e := ensurePatientInScope(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		return platform.Many(ctx, tx, `SELECT `+nokJ+` FROM patient.next_of_kin n WHERE n.patient_id=$1::uuid ORDER BY n.status, n.priority`, patientID)
	})
}

func (s Service) AddNextOfKin(ctx context.Context, a identity.Actor, patientID string, in NextOfKinInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "patient.create", "patient.update"); e != nil {
		return nil, e
	}
	p, e := phone(in.Phone)
	if e != nil || p == "" {
		return nil, platform.Fail(422, "validation", "Phone must be a valid international number")
	}
	alt, e := phone(in.AltPhone)
	if e != nil {
		return nil, e
	}
	prio := in.Priority
	if prio == 0 {
		prio = 1
	}
	emerg := true
	if in.IsEmergencyContact != nil {
		emerg = *in.IsEmergencyContact
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		if e := ensurePatientInScope(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO patient.next_of_kin(org_id,patient_id,full_name,relationship,phone_e164,alt_phone_e164,address,is_emergency_contact,priority)
 VALUES($1::uuid,$2::uuid,$3,$4,$5,NULLIF($6,''),NULLIF($7,''),$8,$9) RETURNING id::text`,
			sess.OrgID, patientID, strings.TrimSpace(in.FullName), in.Relationship, p, alt, strings.TrimSpace(in.Address), emerg, prio).Scan(&id)
		if e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+nokJ+` FROM patient.next_of_kin n WHERE id=$1::uuid`, id)
	})
}

func (s Service) UpdateNextOfKin(ctx context.Context, a identity.Actor, patientID, nokID string, in NextOfKinUpdate, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "patient.update"); e != nil {
		return nil, e
	}
	var p, alt *string
	if in.Phone != nil {
		v, e := phone(*in.Phone)
		if e != nil || v == "" {
			return nil, platform.Fail(422, "validation", "Phone must be a valid international number")
		}
		p = &v
	}
	if in.AltPhone != nil {
		v, e := phone(*in.AltPhone)
		if e != nil {
			return nil, e
		}
		alt = &v
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		if e := ensurePatientInScope(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		tag, e := tx.Exec(ctx, `UPDATE patient.next_of_kin SET
 full_name=coalesce(NULLIF(btrim($3),''),full_name),
 relationship=coalesce($4,relationship),
 phone_e164=coalesce($5,phone_e164),
 alt_phone_e164=CASE WHEN $6::text IS NOT NULL THEN NULLIF($6,'') ELSE alt_phone_e164 END,
 address=CASE WHEN $7::text IS NOT NULL THEN NULLIF(btrim($7),'') ELSE address END,
 is_emergency_contact=coalesce($8,is_emergency_contact),
 priority=coalesce($9,priority),
 status=coalesce($10,status)
 WHERE id=$1::uuid AND patient_id=$2::uuid AND row_version=$11`,
			nokID, patientID, in.FullName, in.Relationship, p, alt, in.Address, in.IsEmergencyContact, in.Priority, in.Status, in.Version)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "patient.next_of_kin", nokID); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+nokJ+` FROM patient.next_of_kin n WHERE id=$1::uuid`, nokID)
	})
}

// ---------------------------------------------------------------- consents

type ConsentInput struct {
	ConsentType         string  `json:"consent_type" validate:"required,oneof=treatment_general telemedicine sms_notifications whatsapp_notifications portal_access data_sharing_insurer research"`
	CaptureMethod       string  `json:"capture_method" validate:"required,oneof=written verbal_witnessed electronic_portal"`
	PolicyVersion       string  `json:"policy_version" validate:"required,min=1,max=64"`
	WitnessMembershipID *string `json:"witness_membership_id" validate:"omitempty,uuid"`
	DocumentID          *string `json:"document_id" validate:"omitempty,uuid"`
}

type WithdrawConsentInput struct {
	Reason  string `json:"reason" validate:"required,min=3,max=500"`
	Version int    `json:"version" validate:"required,min=1"`
}

func (s Service) ListConsents(ctx context.Context, a identity.Actor, patientID, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "patient.read", "clinical.read", "telehealth.schedule"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		if e := ensurePatientInScope(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		return platform.Many(ctx, tx, `SELECT `+consentJ+` FROM patient.consent c WHERE c.patient_id=$1::uuid ORDER BY c.granted_at DESC`, patientID)
	})
}

func (s Service) GrantConsent(ctx context.Context, a identity.Actor, patientID string, in ConsentInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "patient.create", "patient.update", "telehealth.schedule"); e != nil {
		return nil, e
	}
	if in.CaptureMethod == "verbal_witnessed" && in.WitnessMembershipID == nil {
		return nil, platform.Fail(422, "validation", "witness_membership_id is required for verbal witnessed consent")
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		if e := ensurePatientInScope(ctx, tx, a.FacilityIDs, patientID); e != nil {
			return nil, e
		}
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO patient.consent(org_id,patient_id,consent_type,capture_method,witness_membership_id,document_id,policy_version)
 VALUES($1::uuid,$2::uuid,$3,$4,$5::uuid,$6::uuid,$7) RETURNING id::text`,
			sess.OrgID, patientID, in.ConsentType, in.CaptureMethod, in.WitnessMembershipID, in.DocumentID, strings.TrimSpace(in.PolicyVersion)).Scan(&id)
		if e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "patient.consent.grant", "phi_access", "success", "consent", id, patientID, in.ConsentType); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+consentJ+` FROM patient.consent c WHERE id=$1::uuid`, id)
	})
}

func (s Service) WithdrawConsent(ctx context.Context, a identity.Actor, consentID string, in WithdrawConsentInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "patient.update", "telehealth.schedule"); e != nil {
		return nil, e
	}
	reason, e := platform.Reason(in.Reason, 3)
	if e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE patient.consent SET status='withdrawn',withdrawn_at=now(),withdrawal_reason=$3 WHERE id=$1::uuid AND row_version=$2 AND status='granted'`,
			consentID, in.Version, reason)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "patient.consent", consentID); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+consentJ+` FROM patient.consent c WHERE id=$1::uuid`, consentID)
	})
}

// ---------------------------------------------------------------- duplicates & merge

type DuplicateCheckInput struct {
	GivenName  string `json:"given_name" validate:"required,min=1,max=100"`
	FamilyName string `json:"family_name" validate:"required,min=1,max=100"`
	BirthDate  string `json:"birth_date" validate:"required,datetime=2006-01-02"`
	Phone      string `json:"phone" validate:"max=40"`
}

type DuplicateReviewInput struct {
	PatientIDA   string   `json:"patient_id_a" validate:"required,uuid"`
	PatientIDB   string   `json:"patient_id_b" validate:"required,uuid"`
	MatchScore   float64  `json:"match_score" validate:"min=0,max=1"`
	MatchReasons []string `json:"match_reasons" validate:"max=10,dive,max=50"`
}

type DuplicateResolveInput struct {
	Status  string `json:"status" validate:"required,oneof=confirmed_distinct confirmed_duplicate"`
	Note    string `json:"note" validate:"max=500"`
	Version int    `json:"version" validate:"required,min=1"`
}

type MergeInput struct {
	SurvivorPatientID string  `json:"survivor_patient_id" validate:"required,uuid"`
	MergedPatientID   string  `json:"merged_patient_id" validate:"required,uuid"`
	DuplicateReviewID *string `json:"duplicate_review_id" validate:"omitempty,uuid"`
	Reason            string  `json:"reason" validate:"required,min=10,max=500"`
}

func (s Service) CheckDuplicates(ctx context.Context, a identity.Actor, in DuplicateCheckInput, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "patient.read"); e != nil {
		return nil, e
	}
	p, e := phone(in.Phone)
	if e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT jsonb_build_object('patient_id',patient_id,'mrn',mrn,'full_name',full_name,'date_of_birth',date_of_birth,'score',score,'reasons',reasons)
 FROM patient.find_duplicates($1,$2,$3::date,NULLIF($4,''),10)`,
			strings.TrimSpace(in.GivenName), strings.TrimSpace(in.FamilyName), in.BirthDate, p)
	})
}

func (s Service) ListDuplicateReviews(ctx context.Context, a identity.Actor, status, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "patient.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+dupJ+` FROM patient.duplicate_review d WHERE ($1='' OR d.status=$1) ORDER BY d.created_at DESC LIMIT 100`, status)
	})
}

func (s Service) CreateDuplicateReview(ctx context.Context, a identity.Actor, in DuplicateReviewInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "patient.create", "patient.merge"); e != nil {
		return nil, e
	}
	if in.PatientIDA == in.PatientIDB {
		return nil, platform.Fail(422, "validation", "Duplicate review requires two distinct patients")
	}
	pa, pb := in.PatientIDA, in.PatientIDB
	if pa > pb {
		pa, pb = pb, pa
	}
	reasons := in.MatchReasons
	if reasons == nil {
		reasons = []string{}
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var id string
		e := tx.QueryRow(ctx, `INSERT INTO patient.duplicate_review(org_id,patient_id_a,patient_id_b,match_score,match_reasons)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4,$5) RETURNING id::text`, sess.OrgID, pa, pb, in.MatchScore, reasons).Scan(&id)
		if e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+dupJ+` FROM patient.duplicate_review d WHERE id=$1::uuid`, id)
	})
}

func (s Service) ResolveDuplicateReview(ctx context.Context, a identity.Actor, id string, in DuplicateResolveInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "patient.create", "patient.merge"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE patient.duplicate_review SET status=$3,resolved_by=$4::uuid,resolved_at=now(),resolution_note=NULLIF($5,'')
 WHERE id=$1::uuid AND row_version=$2`, id, in.Version, in.Status, sess.UserID, strings.TrimSpace(in.Note))
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "patient.duplicate_review", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+dupJ+` FROM patient.duplicate_review d WHERE id=$1::uuid`, id)
	})
}

func (s Service) MergePatients(ctx context.Context, a identity.Actor, in MergeInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "patient.merge"); e != nil {
		return nil, e
	}
	reason, e := platform.Reason(in.Reason, 10)
	if e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		return platform.One(ctx, tx, `SELECT patient.merge_patients($1::uuid,$2::uuid,$3,$4::uuid)`,
			in.SurvivorPatientID, in.MergedPatientID, reason, in.DuplicateReviewID)
	})
}
