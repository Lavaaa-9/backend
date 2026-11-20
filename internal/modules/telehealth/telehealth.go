// Package telehealth covers clinic appointments and telemedicine sessions/events.
package telehealth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
)

type Service struct{ DB *pgxpool.Pool }
type Raw = platform.Raw

var (
	apptJ  = platform.JSON("a", true)
	teleJ  = platform.JSON("s", true)
	eventJ = `(to_jsonb(e) - 'org_id')`
)

// ---------------------------------------------------------------- appointments

type AppointmentInput struct {
	FacilityID               string  `json:"facility_id" validate:"required,uuid"`
	DepartmentID             string  `json:"department_id" validate:"required,uuid"`
	PatientID                string  `json:"patient_id" validate:"required,uuid"`
	PractitionerMembershipID *string `json:"practitioner_membership_id" validate:"omitempty,uuid"`
	AppointmentType          string  `json:"appointment_type" validate:"omitempty,oneof=in_person telemedicine"`
	Source                   string  `json:"source" validate:"omitempty,oneof=reception patient_app phone follow_up"`
	ScheduledStart           string  `json:"scheduled_start" validate:"required,datetime=2006-01-02T15:04:05Z07:00"`
	ScheduledEnd             string  `json:"scheduled_end" validate:"required,datetime=2006-01-02T15:04:05Z07:00"`
	Reason                   string  `json:"reason" validate:"max=500"`
	Status                   string  `json:"status" validate:"omitempty,oneof=requested booked"`
}

type AppointmentUpdate struct {
	DepartmentID             *string `json:"department_id" validate:"omitempty,uuid"`
	PractitionerMembershipID *string `json:"practitioner_membership_id" validate:"omitempty,uuid"`
	ScheduledStart           *string `json:"scheduled_start" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
	ScheduledEnd             *string `json:"scheduled_end" validate:"omitempty,datetime=2006-01-02T15:04:05Z07:00"`
	Reason                   *string `json:"reason" validate:"omitempty,max=500"`
	Version                  int     `json:"version" validate:"required,min=1"`
}

type AppointmentStatusInput struct {
	Status       string `json:"status" validate:"required,oneof=booked confirmed checked_in completed cancelled no_show rejected"`
	CancelReason string `json:"cancel_reason" validate:"max=500"`
	Version      int    `json:"version" validate:"required,min=1"`
}

func (s Service) CreateAppointment(ctx context.Context, a identity.Actor, in AppointmentInput, key, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "appointment.manage"); e != nil {
		return nil, e
	}
	if !a.InFacility(in.FacilityID) {
		return nil, platform.Forbidden()
	}
	start, e := time.Parse(time.RFC3339, in.ScheduledStart)
	if e != nil {
		return nil, platform.Fail(422, "validation", "Invalid scheduled_start")
	}
	end, e := time.Parse(time.RFC3339, in.ScheduledEnd)
	if e != nil || !end.After(start) || end.Sub(start) > 8*time.Hour {
		return nil, platform.Fail(422, "validation", "scheduled_end must be after scheduled_start and within 8 hours")
	}
	if in.AppointmentType == "" {
		in.AppointmentType = "in_person"
	}
	if in.Source == "" {
		in.Source = "reception"
	}
	if in.Status == "" {
		in.Status = "booked"
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var idemUUID *string
		if _, err := uuid.Parse(key); err == nil {
			idemUUID = &key
		}
		create := func() (Raw, error) {
			var id string
			e := tx.QueryRow(ctx, `INSERT INTO clinical.appointment(org_id,facility_id,department_id,patient_id,practitioner_membership_id,appointment_type,source,scheduled_start,scheduled_end,reason,status,idempotency_key)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,$7,$8,$9,NULLIF($10,''),$11,$12::uuid) RETURNING id::text`,
				sess.OrgID, in.FacilityID, in.DepartmentID, in.PatientID, in.PractitionerMembershipID,
				in.AppointmentType, in.Source, start.UTC(), end.UTC(), strings.TrimSpace(in.Reason), in.Status, idemUUID).Scan(&id)
			if e != nil {
				return nil, e
			}
			// Enqueue an outbox notification event atomically (no PHI in payload).
			if _, e = tx.Exec(ctx, `INSERT INTO comms.outbox(org_id,event_type,aggregate_type,aggregate_id,dedupe_key,payload)
 VALUES($1::uuid,'appointment.booked','appointment',$2::uuid,'appt_booked:'||$3::text,jsonb_build_object('appointment_id',$3::text,'appointment_type',$4::text,'scheduled_start',$5::text))
 ON CONFLICT DO NOTHING`, sess.OrgID, id, id, in.AppointmentType, start.UTC().Format(time.RFC3339)); e != nil {
				return nil, e
			}
			if e = platform.Audit(ctx, tx, "appointment.create", "phi_access", "success", "appointment", id, in.PatientID, ""); e != nil {
				return nil, e
			}
			return platform.One(ctx, tx, `SELECT `+apptJ+` FROM clinical.appointment a WHERE id=$1::uuid`, id)
		}
		if key != "" {
			return platform.Idempotent(ctx, tx, sess, "appointment.create", key, in, create)
		}
		return create()
	})
}

func (s Service) ListAppointments(ctx context.Context, a identity.Actor, patientID, deptID, status, after, request string, limit int) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "appointment.read", "appointment.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+apptJ+` FROM clinical.appointment a
 WHERE a.facility_id=ANY($1::uuid[])
   AND ($2='' OR a.patient_id=NULLIF($2,'')::uuid)
   AND ($3='' OR a.department_id=NULLIF($3,'')::uuid)
   AND ($4='' OR a.status=$4)
   AND ($5='' OR a.id>NULLIF($5,'')::uuid)
 ORDER BY a.scheduled_start DESC, a.id LIMIT $6`, a.FacilityIDs, patientID, deptID, status, after, limit)
	})
}

func (s Service) GetAppointment(ctx context.Context, a identity.Actor, id, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "appointment.read", "appointment.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		return platform.One(ctx, tx, `SELECT `+apptJ+` FROM clinical.appointment a WHERE a.id=$1::uuid AND a.facility_id=ANY($2::uuid[])`, id, a.FacilityIDs)
	})
}

func (s Service) UpdateAppointment(ctx context.Context, a identity.Actor, id string, in AppointmentUpdate, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "appointment.manage"); e != nil {
		return nil, e
	}
	var start, end *time.Time
	if in.ScheduledStart != nil {
		t, e := time.Parse(time.RFC3339, *in.ScheduledStart)
		if e != nil {
			return nil, platform.Fail(422, "validation", "Invalid scheduled_start")
		}
		u := t.UTC()
		start = &u
	}
	if in.ScheduledEnd != nil {
		t, e := time.Parse(time.RFC3339, *in.ScheduledEnd)
		if e != nil {
			return nil, platform.Fail(422, "validation", "Invalid scheduled_end")
		}
		u := t.UTC()
		end = &u
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE clinical.appointment SET
 department_id=coalesce($3::uuid,department_id),
 practitioner_membership_id=coalesce($4::uuid,practitioner_membership_id),
 scheduled_start=coalesce($5,scheduled_start),
 scheduled_end=coalesce($6,scheduled_end),
 reason=CASE WHEN $7::text IS NOT NULL THEN NULLIF(btrim($7),'') ELSE reason END
 WHERE id=$1::uuid AND row_version=$2 AND facility_id=ANY($8::uuid[]) AND status IN ('requested','booked','confirmed')`,
			id, in.Version, in.DepartmentID, in.PractitionerMembershipID, start, end, in.Reason, a.FacilityIDs)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "clinical.appointment", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+apptJ+` FROM clinical.appointment a WHERE id=$1::uuid`, id)
	})
}

func (s Service) ChangeAppointmentStatus(ctx context.Context, a identity.Actor, id string, in AppointmentStatusInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "appointment.manage"); e != nil {
		return nil, e
	}
	reason := strings.TrimSpace(in.CancelReason)
	if (in.Status == "cancelled" || in.Status == "rejected") && reason == "" {
		return nil, platform.Fail(422, "validation", "cancel_reason is required when cancelling or rejecting an appointment")
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE clinical.appointment SET
 status=$3,
 cancel_reason=CASE WHEN $3 IN ('cancelled','rejected') THEN $4 ELSE cancel_reason END,
 cancelled_at=CASE WHEN $3 IN ('cancelled','rejected') THEN now() ELSE cancelled_at END,
 cancelled_by=CASE WHEN $3 IN ('cancelled','rejected') THEN $5::uuid ELSE cancelled_by END,
 checked_in_at=CASE WHEN $3='checked_in' THEN coalesce(checked_in_at,now()) ELSE checked_in_at END
 WHERE id=$1::uuid AND row_version=$2 AND facility_id=ANY($6::uuid[])`,
			id, in.Version, in.Status, reason, sess.UserID, a.FacilityIDs)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "clinical.appointment", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+apptJ+` FROM clinical.appointment a WHERE id=$1::uuid`, id)
	})
}

type CheckInInput struct {
	EncounterType  string `json:"encounter_type" validate:"omitempty,oneof=outpatient emergency telemedicine follow_up"`
	ChiefComplaint string `json:"chief_complaint" validate:"max=500"`
	Version        int    `json:"version" validate:"required,min=1"`
}

// CheckInAppointment atomically checks in a booked/confirmed appointment and creates & links its encounter.
func (s Service) CheckInAppointment(ctx context.Context, a identity.Actor, id string, in CheckInInput, request string) (map[string]any, error) {
	if e := platform.Need(a.Permissions, "appointment.manage"); e != nil {
		return nil, e
	}
	if e := platform.Need(a.Permissions, "encounter.manage"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (map[string]any, error) {
		var patientID, facilityID, apptType, status string
		var apptReason *string
		var rowVer int
		e := tx.QueryRow(ctx, `SELECT patient_id::text,facility_id::text,appointment_type,status,reason,row_version
 FROM clinical.appointment WHERE id=$1::uuid AND facility_id=ANY($2::uuid[]) FOR UPDATE`, id, a.FacilityIDs).
			Scan(&patientID, &facilityID, &apptType, &status, &apptReason, &rowVer)
		if e != nil {
			return nil, e
		}
		if rowVer != in.Version {
			return nil, platform.Conflict()
		}
		if status != "booked" && status != "confirmed" && status != "checked_in" {
			return nil, platform.Fail(409, "conflict", "Appointment cannot be checked in from status "+status)
		}
		if status != "checked_in" {
			if _, e = tx.Exec(ctx, `UPDATE clinical.appointment SET status='checked_in', checked_in_at=coalesce(checked_in_at,now()) WHERE id=$1::uuid`, id); e != nil {
				return nil, e
			}
		}
		var encID string
		e = tx.QueryRow(ctx, `SELECT id::text FROM clinical.encounter WHERE appointment_id=$1::uuid`, id).Scan(&encID)
		if errors.Is(e, pgx.ErrNoRows) {
			encType := in.EncounterType
			if encType == "" {
				if apptType == "telemedicine" {
					encType = "telemedicine"
				} else {
					encType = "outpatient"
				}
			}
			cc := strings.TrimSpace(in.ChiefComplaint)
			if cc == "" && apptReason != nil {
				cc = *apptReason
			}
			if e = tx.QueryRow(ctx, `INSERT INTO clinical.encounter(org_id,facility_id,patient_id,appointment_id,encounter_no,encounter_type,chief_complaint)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,clinical.next_encounter_no($1::uuid),$5,NULLIF($6,'')) RETURNING id::text`,
				sess.OrgID, facilityID, patientID, id, encType, cc).Scan(&encID); e != nil {
				return nil, e
			}
		} else if e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "appointment.check_in", "phi_access", "success", "appointment", id, patientID, encID); e != nil {
			return nil, e
		}
		apptJSON, e := platform.One(ctx, tx, `SELECT `+apptJ+` FROM clinical.appointment a WHERE a.id=$1::uuid`, id)
		if e != nil {
			return nil, e
		}
		encJSON, e := platform.One(ctx, tx, `SELECT `+platform.JSON("e", true)+` FROM clinical.encounter e WHERE e.id=$1::uuid`, encID)
		if e != nil {
			return nil, e
		}
		return map[string]any{"appointment": apptJSON, "encounter": encJSON}, nil
	})
}

// ---------------------------------------------------------------- telehealth sessions

type SessionInput struct {
	AppointmentID            string  `json:"appointment_id" validate:"required,uuid"`
	PractitionerMembershipID *string `json:"practitioner_membership_id" validate:"omitempty,uuid"`
	ConsentID                *string `json:"consent_id" validate:"omitempty,uuid"`
	Provider                 string  `json:"provider" validate:"required,oneof=jitsi_self_hosted daily twilio agora other"`
	ProviderRoomRef          string  `json:"provider_room_ref" validate:"max=160"`
}

type SessionStatusInput struct {
	Status      string  `json:"status" validate:"required,oneof=scheduled waiting_room in_progress completed cancelled failed no_show"`
	EncounterID *string `json:"encounter_id" validate:"omitempty,uuid"`
	EndReason   string  `json:"end_reason" validate:"max=500"`
	Version     int     `json:"version" validate:"required,min=1"`
}

type SessionEventInput struct {
	EventType       string         `json:"event_type" validate:"required,oneof=participant_joined participant_left connection_lost reconnected provider_error ended"`
	ParticipantRole *string        `json:"participant_role" validate:"omitempty,oneof=patient practitioner interpreter guardian"`
	Detail          map[string]any `json:"detail"`
}

func (s Service) CreateSession(ctx context.Context, a identity.Actor, in SessionInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "telehealth.schedule", "telehealth.conduct"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var patientID string
		var apptPract *string
		var schedStart time.Time
		e := tx.QueryRow(ctx, `SELECT patient_id::text,practitioner_membership_id::text,scheduled_start
 FROM clinical.appointment WHERE id=$1::uuid AND facility_id=ANY($2::uuid[])`, in.AppointmentID, a.FacilityIDs).
			Scan(&patientID, &apptPract, &schedStart)
		if e != nil {
			return nil, e
		}
		pract := sess.MembershipID
		if in.PractitionerMembershipID != nil && *in.PractitionerMembershipID != "" {
			pract = *in.PractitionerMembershipID
		} else if apptPract != nil && *apptPract != "" {
			pract = *apptPract
		}
		consentID := ""
		if in.ConsentID != nil && *in.ConsentID != "" {
			consentID = *in.ConsentID
		} else {
			e = tx.QueryRow(ctx, `SELECT id::text FROM patient.consent WHERE patient_id=$1::uuid AND consent_type='telemedicine' AND status='granted' LIMIT 1`, patientID).Scan(&consentID)
			if errors.Is(e, pgx.ErrNoRows) {
				return nil, platform.Fail(422, "consent_required", "Active telemedicine consent for this patient is required")
			}
			if e != nil {
				return nil, e
			}
		}
		roomRef := strings.TrimSpace(in.ProviderRoomRef)
		if roomRef == "" {
			roomRef = "room-" + uuid.NewString()
		}
		var id string
		e = tx.QueryRow(ctx, `INSERT INTO telehealth.session(org_id,appointment_id,patient_id,practitioner_membership_id,consent_id,provider,provider_room_ref,scheduled_start)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,$7,$8) RETURNING id::text`,
			sess.OrgID, in.AppointmentID, patientID, pract, consentID, in.Provider, roomRef, schedStart).Scan(&id)
		if e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "telehealth.session.create", "phi_access", "success", "telehealth_session", id, patientID, ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+teleJ+` FROM telehealth.session s WHERE id=$1::uuid`, id)
	})
}

func (s Service) ListSessions(ctx context.Context, a identity.Actor, patientID, status, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "telehealth.conduct", "telehealth.schedule", "appointment.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		return platform.Many(ctx, tx, `SELECT `+teleJ+` FROM telehealth.session s
 WHERE ($1='' OR s.patient_id=NULLIF($1,'')::uuid) AND ($2='' OR s.status=$2)
 ORDER BY s.scheduled_start DESC LIMIT 100`, patientID, status)
	})
}

func (s Service) GetSession(ctx context.Context, a identity.Actor, id, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "telehealth.conduct", "telehealth.schedule", "appointment.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		return platform.One(ctx, tx, `SELECT `+teleJ+` || jsonb_build_object('events',
 coalesce((SELECT jsonb_agg(`+eventJ+` ORDER BY e.occurred_at) FROM telehealth.session_event e WHERE e.session_id=s.id),'[]'::jsonb))
 FROM telehealth.session s WHERE s.id=$1::uuid`, id)
	})
}

func (s Service) ChangeSessionStatus(ctx context.Context, a identity.Actor, id string, in SessionStatusInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "telehealth.conduct", "telehealth.schedule"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE telehealth.session SET
 status=$3,
 encounter_id=coalesce($4::uuid,encounter_id),
 end_reason=CASE WHEN $5<>'' THEN $5 ELSE end_reason END
 WHERE id=$1::uuid AND row_version=$2`, id, in.Version, in.Status, in.EncounterID, strings.TrimSpace(in.EndReason))
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "telehealth.session", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+teleJ+` FROM telehealth.session s WHERE id=$1::uuid`, id)
	})
}

func (s Service) AddSessionEvent(ctx context.Context, a identity.Actor, sessionID string, in SessionEventInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "telehealth.conduct"); e != nil {
		return nil, e
	}
	var detail []byte
	if in.Detail != nil {
		var e error
		detail, e = json.Marshal(in.Detail)
		if e != nil || len(detail) >= 2048 {
			return nil, platform.Fail(422, "validation", "Event detail must be under 2 KB")
		}
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var ok bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM telehealth.session WHERE id=$1::uuid)`, sessionID).Scan(&ok); e != nil {
			return nil, e
		}
		if !ok {
			return nil, platform.NotFound()
		}
		var id int64
		e := tx.QueryRow(ctx, `INSERT INTO telehealth.session_event(org_id,session_id,event_type,participant_role,actor_user_id,detail)
 VALUES($1::uuid,$2::uuid,$3,$4,$5::uuid,$6::jsonb) RETURNING id`,
			sess.OrgID, sessionID, in.EventType, in.ParticipantRole, sess.UserID, detail).Scan(&id)
		if e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+eventJ+` FROM telehealth.session_event e WHERE e.id=$1`, id)
	})
}
