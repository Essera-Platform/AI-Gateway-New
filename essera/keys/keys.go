// Package keys lets a person create and delete API keys on the Gateway. A key is a Console
// credential: the Gateway reports it to the Console (credential.created, credential.revoked
// on the usage queue) and accepts it on relay only once the Console publishes it in a
// configuration snapshot, by its key id and the SHA-256 of the key. The key itself is shown
// once and stored nowhere; the new-api token row only stands for it in the web UI.
package keys

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/essera/auth"
	"github.com/QuantumNous/new-api/essera/identity"
	"github.com/QuantumNous/new-api/essera/records"
	"github.com/QuantumNous/new-api/essera/settings"
	"github.com/QuantumNous/new-api/essera/snapshot"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

const (
	// A key is "sk-", the 16 hex characters of its id, and 40 random letters and digits
	// (about 238 bits). The id is not secret: it names the key in usage and audit.
	keyPrefix    = "sk-"
	idLength     = 16
	secretLength = 40
	keyLength    = len(keyPrefix) + idLength + secretLength

	// Lifetime is how long a new key works unless a shorter time is chosen.
	Lifetime = 90 * 24 * time.Hour
)

var (
	ErrNotAPerson  = errors.New("Keys are created by the people of a Console customer.")
	ErrExpiryEnded = errors.New("Choose an expiry in the future.")
	ErrNameMissing = errors.New("Give the key a name.")
)

// Created is a new key; Key is shown to its person once and kept nowhere.
type Created struct {
	TokenID   int       `json:"id"`
	KeyID     string    `json:"key_id"`
	Key       string    `json:"key"`
	Name      string    `json:"name"`
	ExpiresAt time.Time `json:"expires_at"`
}

type createdEvent struct {
	Type       string    `json:"type"`
	EventID    string    `json:"event_id"`
	TS         time.Time `json:"ts"`
	KeyID      string    `json:"key_id"`
	KeySHA256  string    `json:"key_sha256"`
	KeyPrefix  string    `json:"key_prefix"`
	Name       string    `json:"name"`
	PersonID   string    `json:"person_id"`
	CustomerID string    `json:"customer_id"`
	EndUserID  *string   `json:"end_user_id"`
	ExpiresAt  time.Time `json:"expires_at"`
}

type revokedEvent struct {
	Type     string    `json:"type"`
	EventID  string    `json:"event_id"`
	TS       time.Time `json:"ts"`
	KeyID    string    `json:"key_id"`
	PersonID string    `json:"person_id"`
	Reason   string    `json:"reason"`
}

// Digest is the hex SHA-256 of a key, as the Console publishes it.
func Digest(key string) string {
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:])
}

// Create makes a key for an active person. requestedExpiry is the chosen end in unix seconds,
// or zero or less for none; a key never lasts longer than Lifetime.
func Create(userID int, name string, requestedExpiry int64, now time.Time) (*Created, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrNameMissing
	}
	person, err := identity.ActivePerson(userID, snapshot.Current())
	if err != nil {
		var refusal *identity.Refusal
		if errors.As(err, &refusal) {
			return nil, ErrNotAPerson
		}
		return nil, err
	}
	now = now.UTC().Truncate(time.Second)
	expiresAt := now.Add(Lifetime)
	if requestedExpiry > 0 {
		chosen := time.Unix(requestedExpiry, 0).UTC()
		if !chosen.After(now) {
			return nil, ErrExpiryEnded
		}
		if chosen.Before(expiresAt) {
			expiresAt = chosen
		}
	}
	idBytes := make([]byte, idLength/2)
	if _, err := rand.Read(idBytes); err != nil {
		return nil, err
	}
	id := hex.EncodeToString(idBytes)
	secret, err := common.GenerateRandomCharsKey(secretLength)
	if err != nil {
		return nil, err
	}
	filler, err := common.GenerateRandomCharsKey(28)
	if err != nil {
		return nil, err
	}
	key := keyPrefix + id + secret
	created := &Created{KeyID: "k_" + id, Key: key, Name: name, ExpiresAt: expiresAt}
	token := model.Token{
		UserId: userID,
		Name:   created.Name,
		// Masks like the key (its id first, its last four characters last), and is unusable:
		// the filler is random and never shown, and relay only accepts published keys.
		Key:            id + filler + key[len(key)-4:],
		Status:         common.TokenStatusEnabled,
		CreatedTime:    now.Unix(),
		AccessedTime:   now.Unix(),
		ExpiredTime:    expiresAt.Unix(),
		UnlimitedQuota: true,
		Group:          settings.Group,
	}
	event := createdEvent{
		Type: "credential.created", EventID: "gw-key-" + created.KeyID + "-created", TS: now,
		KeyID: created.KeyID, KeySHA256: Digest(key), KeyPrefix: key[:len(keyPrefix)+4], Name: created.Name,
		PersonID: person.ID, CustomerID: person.CustomerID, EndUserID: person.EndUserID, ExpiresAt: expiresAt,
	}
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&token).Error; err != nil {
			return err
		}
		row := records.Key{KeyID: created.KeyID, TokenID: token.Id, UserID: userID, PersonID: person.ID, CustomerID: person.CustomerID, ExpiresAt: expiresAt, CreatedAt: now}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		return enqueue(tx, event.EventID, event, now)
	})
	if err != nil {
		return nil, err
	}
	created.TokenID = token.Id
	return created, nil
}

// Revoke deletes the person's keys among tokenIDs, effective on relay at once, and tells the
// Console. It returns how many were deleted.
func Revoke(userID int, tokenIDs []int, now time.Time) (int, error) {
	person, err := identity.IsPerson(userID)
	if err != nil {
		return 0, err
	}
	if !person {
		return 0, ErrNotAPerson
	}
	now = now.UTC()
	deleted := 0
	err = model.DB.Transaction(func(tx *gorm.DB) error {
		var keys []records.Key
		if err := tx.Where("user_id = ? AND token_id IN ? AND revoked_at IS NULL", userID, tokenIDs).Find(&keys).Error; err != nil {
			return err
		}
		result := tx.Where("user_id = ? AND id IN ?", userID, tokenIDs).Delete(&model.Token{})
		if result.Error != nil {
			return result.Error
		}
		deleted = int(result.RowsAffected)
		for _, key := range keys {
			if err := tx.Model(&records.Key{}).Where("key_id = ?", key.KeyID).Update("revoked_at", now).Error; err != nil {
				return err
			}
			event := revokedEvent{Type: "credential.revoked", EventID: "gw-key-" + key.KeyID + "-revoked", TS: now, KeyID: key.KeyID, PersonID: key.PersonID, Reason: "Deleted on the Gateway"}
			if err := enqueue(tx, event.EventID, event, now); err != nil {
				return err
			}
		}
		return nil
	})
	return deleted, err
}

func enqueue(tx *gorm.DB, eventID string, event any, now time.Time) error {
	body, err := common.Marshal(event)
	if err != nil {
		return err
	}
	return tx.Create(&records.OutboxEvent{EventID: eventID, Body: string(body), CreatedAt: now}).Error
}

func invalid() *auth.Failure {
	return &auth.Failure{Code: "essera_unauthenticated", Message: "The API key is not valid."}
}

// Authenticate accepts a key on relay only when the active configuration publishes an API key
// with its id and SHA-256, unexpired, of an active person of the same customer, and the person
// has not deleted it on the Gateway since. Until the key's digest matches, every refusal is the
// same, so nothing tells a key id that exists from one that does not.
func Authenticate(presented string, view *snapshot.View, now time.Time) (*auth.Caller, *auth.Failure) {
	if view == nil {
		return nil, &auth.Failure{Code: "essera_unavailable", Message: "The gateway has no configuration yet."}
	}
	id, ok := strings.CutPrefix(presented, keyPrefix)
	if !ok || len(presented) != keyLength {
		return nil, invalid()
	}
	id = id[:idLength]
	if _, err := hex.DecodeString(id); err != nil || strings.ToLower(id) != id {
		return nil, invalid()
	}
	credential := view.APIKey("k_" + id)
	if credential == nil || subtle.ConstantTimeCompare([]byte(Digest(presented)), []byte(strings.ToLower(credential.KeySHA256))) != 1 {
		return nil, invalid()
	}
	if credential.ExpiresAt != nil && !now.Before(*credential.ExpiresAt) {
		return nil, &auth.Failure{Code: "essera_unauthenticated", Message: "This API key has expired."}
	}
	caller, failure := identity.Caller(view, view.Person(credential.PersonID), credential)
	if failure != nil {
		return nil, failure
	}
	var row records.Key
	err := model.DB.Where("key_id = ? AND person_id = ? AND revoked_at IS NULL", credential.KeyID, credential.PersonID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, &auth.Failure{Code: "essera_unauthenticated", Message: "This API key has been deleted."}
	}
	if err != nil {
		return nil, &auth.Failure{Code: "essera_unavailable", Message: "The gateway cannot check this key right now."}
	}
	return caller, nil
}
