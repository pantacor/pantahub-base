// Copyright 2026  Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
//   Unless required by applicable law or agreed to in writing, software
//   distributed under the License is distributed on an "AS IS" BASIS,
//   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//   See the License for the specific language governing permissions and
//   limitations under the License.
//

package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// WebauthnCredentialsCollection one doc per registered authenticator
	WebauthnCredentialsCollection = "pantahub_webauthn_credentials"

	// WebauthnSessionsCollection short-lived WebAuthn ceremony state
	WebauthnSessionsCollection = "pantahub_webauthn_sessions"
)

// WebauthnCredential is one registered security key or passkey. The embedded
// library credential holds the public key, sign counter, flags and
// attestation data; the envelope adds ownership and display metadata.
type WebauthnCredential struct {
	ID  primitive.ObjectID `json:"-" bson:"_id"`
	Prn string             `json:"prn" bson:"prn"`

	// Owner is the account PRN this credential belongs to
	Owner string `json:"owner" bson:"owner"`

	// CredentialID mirrors Credential.ID for the unique index
	CredentialID []byte `json:"-" bson:"credential_id"`

	// Name is the user-chosen label ("YubiKey 5C", "MacBook Touch ID")
	Name string `json:"name" bson:"name"`

	// IsPasskey is true when registered as a discoverable credential with
	// user verification (usable for passwordless sign-in)
	IsPasskey bool `json:"is_passkey" bson:"is_passkey"`

	Credential webauthn.Credential `json:"-" bson:"credential"`

	TimeCreated time.Time  `json:"time-created" bson:"time-created"`
	LastUsedAt  *time.Time `json:"last_used_at,omitempty" bson:"last_used_at,omitempty"`
}

// WebauthnSession is the server-side state of one in-flight WebAuthn
// ceremony (registration or assertion), single use and TTL'd.
type WebauthnSession struct {
	ID        string    `json:"id" bson:"_id"`
	Owner     string    `json:"owner" bson:"owner"`
	Purpose   string    `json:"purpose" bson:"purpose"`
	IsPasskey bool      `json:"is_passkey" bson:"is_passkey"`
	Data      []byte    `json:"-" bson:"data"`
	ExpiresAt time.Time `json:"expires_at" bson:"expires_at"`
	IsUsed    bool      `json:"is_used" bson:"is_used"`
}

// WebAuthn ceremony purposes
const (
	WebauthnPurposeRegister = "register"
	WebauthnPurposeLogin    = "login"
	WebauthnPurposePasskey  = "passkey"
)

// WebauthnRepo manages WebAuthn credentials and ceremony sessions
type WebauthnRepo struct {
	mongoClient *mongo.Client
	credentials *mongo.Collection
	sessions    *mongo.Collection
}

// NewWebauthnRepo creates a WebauthnRepo bound to the given mongo client
func NewWebauthnRepo(mongoClient *mongo.Client) *WebauthnRepo {
	db := mongoClient.Database(utils.MongoDb)
	return &WebauthnRepo{
		mongoClient: mongoClient,
		credentials: db.Collection(WebauthnCredentialsCollection),
		sessions:    db.Collection(WebauthnSessionsCollection),
	}
}

// SetIndexes creates the owner/credential-id indexes and the session TTL
func (r *WebauthnRepo) SetIndexes(ctx context.Context) error {
	t := true
	var zero int32 = 0

	_, err := r.credentials.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys: bson.D{{Key: "owner", Value: 1}},
		},
		{
			Keys:    bson.D{{Key: "credential_id", Value: 1}},
			Options: &options.IndexOptions{Unique: &t},
		},
	})
	if err != nil {
		return fmt.Errorf("error setting up index for %s: %s", WebauthnCredentialsCollection, err.Error())
	}

	_, err = r.sessions.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "expires_at", Value: 1}},
		Options: &options.IndexOptions{ExpireAfterSeconds: &zero},
	})
	if err != nil {
		return fmt.Errorf("error setting up index for %s: %s", WebauthnSessionsCollection, err.Error())
	}

	return nil
}

// CreateCredential stores a freshly registered credential
func (r *WebauthnRepo) CreateCredential(ctx context.Context, c *WebauthnCredential) error {
	if c.ID.IsZero() {
		c.ID = primitive.NewObjectID()
		c.Prn = utils.IDGetPrn(c.ID, "webauthn-credentials")
	}
	c.CredentialID = c.Credential.ID
	c.TimeCreated = time.Now()

	_, err := r.credentials.InsertOne(ctx, c)
	if mongo.IsDuplicateKeyError(err) {
		return ErrMFAReplayed
	}
	return err
}

// ListByOwner returns all credentials of an account
func (r *WebauthnRepo) ListByOwner(ctx context.Context, ownerPrn string) ([]WebauthnCredential, error) {
	creds := []WebauthnCredential{}
	cursor, err := r.credentials.Find(ctx, bson.M{"owner": ownerPrn},
		options.Find().SetSort(bson.D{{Key: "time-created", Value: 1}}))
	if err != nil {
		return nil, err
	}
	err = cursor.All(ctx, &creds)
	return creds, err
}

// CountByOwner counts an account's registered credentials
func (r *WebauthnRepo) CountByOwner(ctx context.Context, ownerPrn string) (int64, error) {
	return r.credentials.CountDocuments(ctx, bson.M{"owner": ownerPrn})
}

// GetByCredentialID resolves a credential by its WebAuthn credential id
func (r *WebauthnRepo) GetByCredentialID(ctx context.Context, credentialID []byte) (*WebauthnCredential, error) {
	c := &WebauthnCredential{}
	err := r.credentials.FindOne(ctx, bson.M{"credential_id": credentialID}).Decode(c)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// RenameCredential updates the user label of an owned credential
func (r *WebauthnRepo) RenameCredential(ctx context.Context, ownerPrn, id, name string) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return mongo.ErrNoDocuments
	}

	res, err := r.credentials.UpdateOne(ctx,
		bson.M{"_id": objectID, "owner": ownerPrn},
		bson.M{"$set": bson.M{"name": name}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// DeleteCredential removes an owned credential permanently (the same
// authenticator can be registered again later)
func (r *WebauthnRepo) DeleteCredential(ctx context.Context, ownerPrn, id string) error {
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return mongo.ErrNoDocuments
	}

	res, err := r.credentials.DeleteOne(ctx, bson.M{"_id": objectID, "owner": ownerPrn})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// UpdateCredentialAfterLogin persists the post-assertion authenticator state
// (sign counter, clone warning, backup flags) and stamps last use.
//
// prevSignCount is the counter value the assertion was validated against.
// When the new counter is greater than zero the update carries a
// compare-and-set predicate on that prior value, so two assertions that race
// against the same stored counter cannot both persist — the loser modifies
// no document and gets ErrMFAReplayed. Authenticators that never increment
// (SignCount == 0, typical of synced passkeys) skip the CAS, since a
// stationary counter is legitimate for them.
func (r *WebauthnRepo) UpdateCredentialAfterLogin(ctx context.Context, c *WebauthnCredential, prevSignCount uint32) error {
	now := time.Now()
	c.LastUsedAt = &now

	// CAS whenever either the stored or the new counter is non-zero. A
	// stationary-zero counter carries no information (some authenticators
	// never implement one) and cannot be defended by a counter at all - that
	// is an inherent WebAuthn property, not something a predicate can fix.
	cas := prevSignCount > 0 || c.Credential.Authenticator.SignCount > 0
	query := bson.M{"_id": c.ID}
	if cas {
		query["credential.authenticator.signcount"] = prevSignCount
	}

	res, err := r.credentials.UpdateOne(ctx, query,
		bson.M{"$set": bson.M{
			"credential":   c.Credential,
			"last_used_at": now,
		}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 && cas {
		return ErrMFAReplayed
	}
	return nil
}

// CreateSession stores ceremony state and returns its opaque session id
func (r *WebauthnRepo) CreateSession(ctx context.Context, ownerPrn, purpose string, isPasskey bool, data []byte, ttl time.Duration) (string, error) {
	idBytes := make([]byte, 24)
	if _, err := rand.Read(idBytes); err != nil {
		return "", err
	}

	session := &WebauthnSession{
		ID:        hex.EncodeToString(idBytes),
		Owner:     ownerPrn,
		Purpose:   purpose,
		IsPasskey: isPasskey,
		Data:      data,
		ExpiresAt: time.Now().Add(ttl),
		IsUsed:    false,
	}

	if _, err := r.sessions.InsertOne(ctx, session); err != nil {
		return "", err
	}

	return session.ID, nil
}

// ConsumeSession atomically claims a not-yet-used, not-expired ceremony
// session. A second consumption fails with ErrMFAReplayed.
func (r *WebauthnRepo) ConsumeSession(ctx context.Context, id, purpose string) (*WebauthnSession, error) {
	session := &WebauthnSession{}
	err := r.sessions.FindOneAndUpdate(ctx,
		bson.M{
			"_id":        id,
			"purpose":    purpose,
			"is_used":    false,
			"expires_at": bson.M{"$gt": time.Now()},
		},
		bson.M{"$set": bson.M{"is_used": true}},
	).Decode(session)
	if err == mongo.ErrNoDocuments {
		return nil, ErrMFAReplayed
	}
	if err != nil {
		return nil, err
	}
	return session, nil
}
