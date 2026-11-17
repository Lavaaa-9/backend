package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Keyring performs authenticated field-level encryption (AES-256-GCM) for secrets and,
// later, selected PHI columns. Keys come from configuration/KMS, never from the database.
// Ciphertext format: 0x01 | len(keyID) | keyID | 12-byte nonce | GCM ciphertext+tag.
// The key id travels with the ciphertext so keys can be rotated without re-encrypting at once.
type Keyring struct {
	active string
	keys   map[string]cipher.AEAD
}

var keyID = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,31}$`)

var ErrDecrypt = errors.New("ciphertext could not be authenticated")

// ParseKeyring reads "id:base64(32 bytes),id2:base64(32 bytes)" and the active id.
func ParseKeyring(spec, active string) (*Keyring, error) {
	k := &Keyring{active: active, keys: map[string]cipher.AEAD{}}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		id, encoded, ok := strings.Cut(part, ":")
		if !ok || !keyID.MatchString(id) {
			return nil, errors.New("encryption keys must be id:base64 pairs")
		}
		raw, e := base64.StdEncoding.DecodeString(encoded)
		if e != nil || len(raw) != 32 {
			return nil, fmt.Errorf("encryption key %q must be 32 random bytes, base64 encoded", id)
		}
		if _, dup := k.keys[id]; dup {
			return nil, fmt.Errorf("duplicate encryption key id %q", id)
		}
		block, e := aes.NewCipher(raw)
		if e != nil {
			return nil, e
		}
		aead, e := cipher.NewGCM(block)
		if e != nil {
			return nil, e
		}
		k.keys[id] = aead
	}
	if _, ok := k.keys[active]; !ok {
		return nil, errors.New("active encryption key id is not in the keyring")
	}
	return k, nil
}

// Seal encrypts with the active key. aad binds the ciphertext to its owner (table:row id).
func (k *Keyring) Seal(plaintext, aad []byte) ([]byte, error) {
	aead := k.keys[k.active]
	nonce := make([]byte, aead.NonceSize())
	if _, e := rand.Read(nonce); e != nil {
		return nil, e
	}
	out := make([]byte, 0, 2+len(k.active)+len(nonce)+len(plaintext)+aead.Overhead())
	n := len(k.active)
	if n < 1 || n > 32 {
		return nil, errors.New("invalid encryption key id")
	}
	out = append(out, 1, byte(n))
	out = append(out, k.active...)
	out = append(out, nonce...)
	return aead.Seal(out, nonce, plaintext, aad), nil
}

// Open decrypts with whichever key id the ciphertext names.
func (k *Keyring) Open(blob, aad []byte) ([]byte, error) {
	if len(blob) < 2 || blob[0] != 1 {
		return nil, ErrDecrypt
	}
	n := int(blob[1])
	if len(blob) < 2+n {
		return nil, ErrDecrypt
	}
	aead, ok := k.keys[string(blob[2:2+n])]
	if !ok {
		return nil, ErrDecrypt
	}
	rest := blob[2+n:]
	if len(rest) < aead.NonceSize()+aead.Overhead() {
		return nil, ErrDecrypt
	}
	pt, e := aead.Open(nil, rest[:aead.NonceSize()], rest[aead.NonceSize():], aad)
	if e != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

// ActiveKeyID reports which key new ciphertext uses (for rotation jobs; never log key material).
func (k *Keyring) ActiveKeyID() string { return k.active }

// NewKeyMaterial returns a fresh base64 32-byte key for provisioning scripts.
func NewKeyMaterial() (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return base64.StdEncoding.EncodeToString(b), nil
}

// CiphertextKeyID returns only non-secret envelope metadata, or empty for a malformed header.
func CiphertextKeyID(blob []byte) string {
	if len(blob) < 2 || blob[0] != 1 {
		return ""
	}
	n := int(blob[1])
	if n == 0 || len(blob) < 2+n {
		return ""
	}
	return string(blob[2 : 2+n])
}
