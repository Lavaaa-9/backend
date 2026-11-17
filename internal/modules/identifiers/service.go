package identifiers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"spartial.local/backend/internal/modules/identity"
	"spartial.local/backend/internal/modules/privacy"
	"spartial.local/backend/internal/platform"
	"spartial.local/backend/internal/security"
	"strings"
	"time"
)

type Service struct {
	DB            *pgxpool.Pool
	Keys          *security.Keyring
	IndexKey      []byte
	PolicyEnabled bool
}
type Input struct {
	Kind   string `json:"kind" validate:"required,oneof=nida insurance"`
	Issuer string `json:"issuer" validate:"required,max=100"`
	Value  string `json:"value" validate:"required,max=128"`
}
type Metadata struct {
	ID        string    `json:"id"`
	PatientID string    `json:"patient_id"`
	Kind      string    `json:"kind"`
	Issuer    string    `json:"issuer"`
	CreatedAt time.Time `json:"created_at"`
}
type Identifier struct {
	Metadata
	Value string `json:"value"`
}

func AAD(id, patient, kind, issuer string) []byte {
	return []byte("patient_identifiers:" + id + ":" + patient + ":" + kind + ":" + issuer)
}
func (s Service) available() error {
	if s.Keys == nil || len(s.IndexKey) != 32 {
		return platform.Fail(503, "identifiers_not_configured", "Identifier encryption and lookup keys are required")
	}
	return nil
}
func normalize(in Input) (Input, error) {
	in.Issuer = strings.TrimSpace(in.Issuer)
	in.Value = strings.ToUpper(strings.TrimSpace(in.Value))
	if in.Issuer == "" || in.Value == "" {
		return in, platform.Fail(422, "validation", "Identifier issuer and value are required")
	}
	return in, nil
}
func (s Service) index(in Input) string {
	mac := hmac.New(sha256.New, s.IndexKey)
	mac.Write([]byte(in.Kind + "\x00" + in.Issuer + "\x00" + in.Value))
	return hex.EncodeToString(mac.Sum(nil))
}
func (s Service) Add(ctx context.Context, a identity.Actor, patient string, in Input, key, request string) (Metadata, error) {
	if !a.Can("patients.write") {
		return Metadata{}, platform.Forbidden()
	}
	if e := s.available(); e != nil {
		return Metadata{}, e
	}
	in, e := normalize(in)
	if e != nil {
		return Metadata{}, e
	}
	tx, e := platform.BeginActor(ctx, s.DB, a.ID, s.PolicyEnabled)
	if e != nil {
		return Metadata{}, e
	}
	defer tx.Rollback(ctx)
	if e = (privacy.Policy{DB: s.DB, Enabled: s.PolicyEnabled}).CanAccessPatient(privacy.WithRequest(ctx, request), tx, a, patient, true); e != nil {
		return Metadata{}, e
	}
	index := s.index(in)
	command, e := platform.BeginCommand(ctx, tx, a.ID, "patient.identifier.add", key, struct{ Patient, Kind, Issuer, Index string }{patient, in.Kind, in.Issuer, index})
	if e != nil {
		return Metadata{}, e
	}
	if command.Replay != nil {
		var out Metadata
		e = json.Unmarshal(command.Replay, &out)
		return out, e
	}
	id := uuid.NewString()
	blob, e := s.Keys.Seal([]byte(in.Value), AAD(id, patient, in.Kind, in.Issuer))
	if e != nil {
		return Metadata{}, e
	}
	out, e := insert(ctx, tx, id, patient, a.ID, in, index, blob)
	if e != nil {
		return Metadata{}, e
	}
	if e = platform.Audit(ctx, tx, a.ID, "patient.identifier.add", id, patient, request, "success"); e != nil {
		return Metadata{}, e
	}
	if e = command.Save(ctx, tx, out); e != nil {
		return Metadata{}, e
	}
	return out, tx.Commit(ctx)
}
func (s Service) List(ctx context.Context, a identity.Actor, patient, request string) ([]Identifier, error) {
	if !a.Can("patients.read") {
		return nil, platform.Forbidden()
	}
	if e := s.available(); e != nil {
		return nil, e
	}
	tx, e := platform.BeginActor(ctx, s.DB, a.ID, s.PolicyEnabled)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if e = (privacy.Policy{DB: s.DB, Enabled: s.PolicyEnabled}).CanAccessPatient(privacy.WithRequest(ctx, request), tx, a, patient, false); e != nil {
		return nil, e
	}
	rows, e := read(ctx, tx, patient)
	if e != nil {
		return nil, e
	}
	out := []Identifier{}
	for _, row := range rows {
		value, e := s.Keys.Open(row.Blob, AAD(row.ID, patient, row.Kind, row.Issuer))
		if e != nil {
			return nil, platform.Fail(503, "identifier_unavailable", "Identifier ciphertext could not be authenticated")
		}
		out = append(out, Identifier{row.Metadata, string(value)})
	}
	if e = platform.Audit(ctx, tx, a.ID, "patient.identifiers.read", patient, patient, request, "success"); e != nil {
		return nil, e
	}
	return out, tx.Commit(ctx)
}
func (s Service) Search(ctx context.Context, a identity.Actor, in Input, request string) ([]string, error) {
	if !a.Can("patients.read") {
		return nil, platform.Forbidden()
	}
	if e := s.available(); e != nil {
		return nil, e
	}
	in, e := normalize(in)
	if e != nil {
		return nil, e
	}
	tx, e := platform.BeginActor(ctx, s.DB, a.ID, s.PolicyEnabled)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	ids, e := lookup(ctx, tx, in, s.index(in))
	if e != nil {
		return nil, e
	}
	if e = platform.AuditPatientReads(ctx, tx, a.ID, "patient.identifier.search_hit", ids, request); e != nil {
		return nil, e
	}
	if e = platform.Audit(ctx, tx, a.ID, "patient.identifier.search", "", "", request, "success"); e != nil {
		return nil, e
	}
	return ids, tx.Commit(ctx)
}
