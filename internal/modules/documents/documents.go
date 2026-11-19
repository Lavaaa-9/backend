// Package documents manages clinical/patient document metadata, lifecycle, legal hold, and audited downloads.
package documents

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
)

type Service struct {
	DB      *pgxpool.Pool
	Keys    *security.Keyring
	BlobDir string
}
type Raw = platform.Raw

const docJ = `((to_jsonb(d) - 'org_id' - 'created_by' - 'updated_by' - 'row_version' - 'sha256' - 'encryption_key_ref') || jsonb_build_object('version',d.row_version,'sha256_hex',encode(d.sha256,'hex')))`

type CreateInput struct {
	EncounterID   *string `json:"encounter_id" validate:"omitempty,uuid"`
	Category      string  `json:"category" validate:"required,oneof=identity referral_letter lab_report imaging consent_form insurance clinical_photo discharge_summary prescription other"`
	Title         string  `json:"title" validate:"required,min=1,max=200"`
	ContentType   string  `json:"content_type" validate:"required,oneof=application/pdf image/jpeg image/png image/webp application/dicom text/plain"`
	SizeBytes     int64   `json:"size_bytes" validate:"omitempty,min=1,max=52428800"`
	SHA256Hex     string  `json:"sha256_hex" validate:"omitempty,len=64,hexadecimal"`
	ContentBase64 string  `json:"content_base64" validate:"omitempty,max=56000"`
}

type StatusInput struct {
	Status    *string `json:"status" validate:"omitempty,oneof=scanning available quarantined"`
	LegalHold *bool   `json:"legal_hold"`
	Version   int     `json:"version" validate:"required,min=1"`
}

type DeleteInput struct {
	Reason  string `json:"reason" validate:"required,min=5,max=500"`
	Version int    `json:"version" validate:"required,min=1"`
}

func (s Service) bucket() string { return "spartial-documents" }

func (s Service) blobPath(orgID, docID string) string {
	dir := s.BlobDir
	if dir == "" {
		dir = ".local/documents"
	}
	return filepath.Join(dir, orgID, docID+".bin")
}

func (s Service) Register(ctx context.Context, a identity.Actor, patientID string, in CreateInput, key, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "document.upload"); e != nil {
		return nil, e
	}
	var rawBytes []byte
	var sum []byte
	sizeBytes := in.SizeBytes
	if in.ContentBase64 != "" {
		b, e := base64.StdEncoding.DecodeString(in.ContentBase64)
		if e != nil || len(b) == 0 {
			return nil, platform.Fail(422, "validation", "content_base64 must be valid non-empty base64")
		}
		rawBytes = b
		sizeBytes = int64(len(b))
		h := sha256.Sum256(b)
		sum = h[:]
	} else {
		if sizeBytes < 1 || in.SHA256Hex == "" {
			return nil, platform.Fail(422, "validation", "Provide either content_base64 or both size_bytes and sha256_hex")
		}
		b, e := hex.DecodeString(in.SHA256Hex)
		if e != nil || len(b) != 32 {
			return nil, platform.Fail(422, "validation", "sha256_hex must be 64 hex characters")
		}
		sum = b
	}
	keyRef := "dev-local"
	if s.Keys != nil {
		keyRef = s.Keys.ActiveKeyID()
	}

	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var ok bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patient.patient WHERE id=$1::uuid AND registered_facility_id=ANY($2::uuid[]))`, patientID, a.FacilityIDs).Scan(&ok); e != nil {
			return nil, e
		}
		if !ok {
			return nil, platform.NotFound()
		}
		create := func() (Raw, error) {
			docID := uuid.NewString()
			storageKey := sess.OrgID + "/" + docID
			_, e := tx.Exec(ctx, `INSERT INTO document.document(id,org_id,patient_id,encounter_id,category,title,storage_bucket,storage_key,content_type,size_bytes,sha256,encryption_key_ref,status)
 VALUES($1::uuid,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,$8,$9,$10,$11,$12,'pending_upload')`,
				docID, sess.OrgID, patientID, in.EncounterID, in.Category, strings.TrimSpace(in.Title),
				s.bucket(), storageKey, in.ContentType, sizeBytes, sum, keyRef)
			if e != nil {
				return nil, e
			}
			if len(rawBytes) > 0 {
				toWrite := rawBytes
				if s.Keys != nil {
					ct, e := s.Keys.Seal(rawBytes, []byte("document:"+sess.OrgID+":"+docID))
					if e != nil {
						return nil, e
					}
					toWrite = ct
				}
				p := s.blobPath(sess.OrgID, docID)
				if e := os.MkdirAll(filepath.Dir(p), 0o700); e != nil {
					return nil, e
				}
				if e := os.WriteFile(p, toWrite, 0o600); e != nil {
					return nil, e
				}
				// Move through pending_upload -> scanning -> available for inline dev uploads.
				if _, e = tx.Exec(ctx, `UPDATE document.document SET status='scanning' WHERE id=$1::uuid`, docID); e != nil {
					return nil, e
				}
				if _, e = tx.Exec(ctx, `UPDATE document.document SET status='available' WHERE id=$1::uuid`, docID); e != nil {
					return nil, e
				}
			}
			if e = platform.Audit(ctx, tx, "document.upload", "phi_access", "success", "document", docID, patientID, in.Category); e != nil {
				return nil, e
			}
			return platform.One(ctx, tx, `SELECT `+docJ+` FROM document.document d WHERE d.id=$1::uuid`, docID)
		}
		if key != "" {
			return platform.Idempotent(ctx, tx, sess, "document.register:"+patientID, key, map[string]any{"category": in.Category, "title": in.Title, "sha256": hex.EncodeToString(sum), "size": sizeBytes}, create)
		}
		return create()
	})
}

func (s Service) List(ctx context.Context, a identity.Actor, patientID string, includeDeleted bool, request string) ([]Raw, error) {
	if e := platform.Need(a.Permissions, "document.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) ([]Raw, error) {
		var ok bool
		if e := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM patient.patient WHERE id=$1::uuid AND registered_facility_id=ANY($2::uuid[]))`, patientID, a.FacilityIDs).Scan(&ok); e != nil {
			return nil, e
		}
		if !ok {
			return nil, platform.NotFound()
		}
		out, e := platform.Many(ctx, tx, `SELECT `+docJ+` FROM document.document d
 WHERE d.patient_id=$1::uuid AND ($2 OR d.status<>'deleted') ORDER BY d.created_at DESC LIMIT 100`, patientID, includeDeleted)
		if e != nil {
			return nil, e
		}
		return out, platform.Audit(ctx, tx, "document.list", "phi_access", "success", "patient", patientID, patientID, "")
	})
}

func (s Service) Get(ctx context.Context, a identity.Actor, id, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "document.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		var patientID string
		if e := tx.QueryRow(ctx, `SELECT patient_id::text FROM document.document WHERE id=$1::uuid`, id).Scan(&patientID); e != nil {
			return nil, e
		}
		if e := platform.Audit(ctx, tx, "document.metadata.view", "phi_access", "success", "document", id, patientID, ""); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+docJ+` FROM document.document d WHERE d.id=$1::uuid`, id)
	})
}

func (s Service) UpdateStatus(ctx context.Context, a identity.Actor, id string, in StatusInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "document.upload", "document.delete"); e != nil {
		return nil, e
	}
	if in.Status == nil && in.LegalHold == nil {
		return nil, platform.Fail(422, "validation", "Provide status and/or legal_hold")
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, _ platform.Session) (Raw, error) {
		tag, e := tx.Exec(ctx, `UPDATE document.document SET status=coalesce($3,status), legal_hold=coalesce($4,legal_hold)
 WHERE id=$1::uuid AND row_version=$2`, id, in.Version, in.Status, in.LegalHold)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "document.document", id); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+docJ+` FROM document.document d WHERE d.id=$1::uuid`, id)
	})
}

func (s Service) Delete(ctx context.Context, a identity.Actor, id string, in DeleteInput, request string) (Raw, error) {
	if e := platform.Need(a.Permissions, "document.delete"); e != nil {
		return nil, e
	}
	reason, e := platform.Reason(in.Reason, 5)
	if e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (Raw, error) {
		var patientID string
		var hold bool
		if e := tx.QueryRow(ctx, `SELECT patient_id::text,legal_hold FROM document.document WHERE id=$1::uuid FOR UPDATE`, id).Scan(&patientID, &hold); e != nil {
			return nil, e
		}
		if hold {
			return nil, platform.Fail(409, "legal_hold", "Document is under legal hold and cannot be deleted")
		}
		tag, e := tx.Exec(ctx, `UPDATE document.document SET status='deleted',deleted_at=now(),deleted_by=$3::uuid,delete_reason=$4
 WHERE id=$1::uuid AND row_version=$2`, id, in.Version, sess.UserID, reason)
		if e != nil {
			return nil, e
		}
		if e = platform.Changed(ctx, tx, tag, "document.document", id); e != nil {
			return nil, e
		}
		if e = platform.Audit(ctx, tx, "document.delete", "phi_access", "success", "document", id, patientID, reason); e != nil {
			return nil, e
		}
		return platform.One(ctx, tx, `SELECT `+docJ+` FROM document.document d WHERE d.id=$1::uuid`, id)
	})
}

func (s Service) Download(ctx context.Context, a identity.Actor, id, request string) (map[string]any, error) {
	if e := platform.Need(a.Permissions, "document.read"); e != nil {
		return nil, e
	}
	return platform.Run(ctx, s.DB, a.SessionID, request, func(tx pgx.Tx, sess platform.Session) (map[string]any, error) {
		var patientID, title, ct, bucket, skey, status, shaHex string
		var size int64
		e := tx.QueryRow(ctx, `SELECT patient_id::text,title,content_type,storage_bucket,storage_key,status,encode(sha256,'hex'),size_bytes
 FROM document.document WHERE id=$1::uuid`, id).Scan(&patientID, &title, &ct, &bucket, &skey, &status, &shaHex, &size)
		if e != nil {
			return nil, e
		}
		if status != "available" {
			return nil, platform.Fail(409, "document_unavailable", "Document is not available for download")
		}
		if e = platform.Audit(ctx, tx, "document.file.download", "phi_access", "success", "document", id, patientID, ""); e != nil {
			return nil, e
		}
		out := map[string]any{
			"id":             id,
			"patient_id":     patientID,
			"title":          title,
			"content_type":   ct,
			"size_bytes":     size,
			"sha256_hex":     shaHex,
			"storage_bucket": bucket,
			"storage_key":    skey,
		}
		if raw, err := os.ReadFile(s.blobPath(sess.OrgID, id)); err == nil {
			if s.Keys != nil {
				if pt, decErr := s.Keys.Open(raw, []byte("document:"+sess.OrgID+":"+id)); decErr == nil {
					raw = pt
				}
			}
			out["content_base64"] = base64.StdEncoding.EncodeToString(raw)
		}
		return out, nil
	})
}
