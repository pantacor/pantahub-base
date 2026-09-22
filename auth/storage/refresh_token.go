// Copyright (c) 2026 Pantacor Ltd.
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
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/storageutils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	// RefreshTokenCollection holds OAuth refresh tokens, hashed.
	//#nosec G101 -- a mongo collection name, not a credential
	RefreshTokenCollection = "oauth_refresh_tokens"

	// refreshReuseGrace is how long after a rotation the previous token may be
	// presented again without it being read as theft. A client whose refresh
	// response was lost retries with the token it still has; that must fail,
	// but it must not take a healthy session down with it.
	refreshReuseGrace = 30 * time.Second
)

var (
	// ErrRefreshTokenInvalid covers every reason a refresh token is not
	// honoured. Callers answer all of them with invalid_grant and say no more.
	ErrRefreshTokenInvalid = errors.New("refresh token is invalid, expired or revoked")
)

// RefreshToken is one link in a chain of rotated refresh tokens. The secret is
// never stored: TokenHash is what secretstore keeps for every machine secret.
type RefreshToken struct {
	ID        primitive.ObjectID `bson:"_id"`
	TokenHash string             `bson:"token_hash"`

	// FamilyID is shared by every token descended from one authorization. It
	// is what gets revoked when a used token is presented again.
	FamilyID string `bson:"family_id"`

	ClientID string `bson:"client_id"`
	UserPrn  string `bson:"user_prn"`
	Scope    string `bson:"scope"`
	Resource string `bson:"resource,omitempty"`

	// ClientName is what the client called itself when the user consented. It
	// is kept here so that listing a user's connections never has to fetch a
	// client's metadata document again.
	ClientName string `bson:"client_name,omitempty"`

	// GrantedAt is when the user consented. It is carried from one token of the
	// family to the next, while CreatedAt is when this token was issued, which
	// is the last time the connection was used to refresh.
	GrantedAt time.Time `bson:"granted_at,omitempty"`

	CreatedAt time.Time  `bson:"created_at"`
	ExpiresAt time.Time  `bson:"expires_at"`
	UsedAt    *time.Time `bson:"used_at,omitempty"`
	Revoked   bool       `bson:"revoked,omitempty"`
}

// RefreshTokenRepo manages refresh tokens in MongoDB.
type RefreshTokenRepo struct {
	collection *mongo.Collection
}

var (
	refreshTokenRepo     *RefreshTokenRepo
	refreshTokenRepoLock sync.Mutex
)

// GetRefreshTokenRepo returns the singleton repo, creating its indexes once.
func GetRefreshTokenRepo() (*RefreshTokenRepo, error) {
	refreshTokenRepoLock.Lock()
	defer refreshTokenRepoLock.Unlock()

	if refreshTokenRepo != nil {
		return refreshTokenRepo, nil
	}

	st, err := storageutils.New("pantahub_")
	if err != nil {
		return nil, err
	}
	repo, err := NewRefreshTokenRepo(st.GetCollection(RefreshTokenCollection))
	if err != nil {
		return nil, err
	}

	refreshTokenRepo = repo
	return refreshTokenRepo, nil
}

// NewRefreshTokenRepo builds a repo over collection and ensures its indexes.
func NewRefreshTokenRepo(collection *mongo.Collection) (*RefreshTokenRepo, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "token_hash", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "family_id", Value: 1}},
		},
		{
			// Mongo removes a token once it has expired, so the collection
			// only ever holds what could still be honoured.
			Keys:    bson.D{{Key: "expires_at", Value: 1}},
			Options: options.Index().SetExpireAfterSeconds(0),
		},
	})
	if err != nil {
		return nil, err
	}

	return &RefreshTokenRepo{collection: collection}, nil
}

func newOpaqueToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// Grant describes the authorization a refresh token stands for.
type Grant struct {
	// FamilyID identifies the connection. It is not a credential: knowing it
	// refreshes nothing. It names the connection to its owner, in the access
	// tokens issued under it and when the owner ends it.
	FamilyID   string
	ClientID   string
	ClientName string
	UserPrn    string
	Scope      string
	Resource   string
	GrantedAt  time.Time
}

// NewFamilyID returns the identifier of a new connection.
func NewFamilyID() (string, error) {
	return newOpaqueToken()
}

// Issue stores a new refresh token for grant and returns its secret, which
// exists nowhere else.
func (r *RefreshTokenRepo) Issue(ctx context.Context, grant Grant, ttl time.Duration) (string, error) {
	if grant.FamilyID == "" {
		return "", errors.New("refresh token without a family")
	}
	secret, err := newOpaqueToken()
	if err != nil {
		return "", err
	}

	now := time.Now()
	if grant.GrantedAt.IsZero() {
		grant.GrantedAt = now
	}
	_, err = r.collection.InsertOne(ctx, &RefreshToken{
		ID:         primitive.NewObjectID(),
		TokenHash:  utils.HashSecret(secret),
		FamilyID:   grant.FamilyID,
		ClientID:   grant.ClientID,
		ClientName: grant.ClientName,
		UserPrn:    grant.UserPrn,
		Scope:      grant.Scope,
		Resource:   grant.Resource,
		GrantedAt:  grant.GrantedAt,
		CreatedAt:  now,
		ExpiresAt:  now.Add(ttl),
	})
	if err != nil {
		return "", err
	}

	return secret, nil
}

// live selects the one token of a family that can still be redeemed. Rotation
// leaves exactly one per connection, so it is also what a connection is.
func live(filter bson.M) bson.M {
	filter["used_at"] = bson.M{"$exists": false}
	filter["revoked"] = bson.M{"$ne": true}
	filter["expires_at"] = bson.M{"$gt": time.Now()}
	return filter
}

// ListActive returns the live connections of an account, newest first.
func (r *RefreshTokenRepo) ListActive(ctx context.Context, userPrn string) ([]RefreshToken, error) {
	if userPrn == "" {
		return nil, errors.New("connections of nobody")
	}
	cur, err := r.collection.Find(ctx, live(bson.M{"user_prn": userPrn}),
		options.Find().
			SetSort(bson.D{{Key: "granted_at", Value: -1}}).
			SetLimit(200).
			SetProjection(bson.M{"token_hash": 0}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)

	connections := []RefreshToken{}
	if err := cur.All(ctx, &connections); err != nil {
		return nil, err
	}
	return connections, nil
}

// RevokeUserFamily ends one connection of an account, and reports whether
// there was such a connection. The account is part of the match, so nobody can
// end a connection that is not theirs by guessing its id.
func (r *RefreshTokenRepo) RevokeUserFamily(ctx context.Context, userPrn, familyID string) (bool, error) {
	if userPrn == "" || familyID == "" {
		return false, nil
	}
	result, err := r.collection.UpdateMany(ctx,
		bson.M{"user_prn": userPrn, "family_id": familyID, "revoked": bson.M{"$ne": true}},
		bson.M{"$set": bson.M{"revoked": true}})
	if err != nil {
		return false, err
	}
	return result.ModifiedCount > 0, nil
}

// FamilyActive reports whether a connection still stands. A resource checks it
// for the access tokens it is shown, because those are self-contained and
// would otherwise keep working until they expire after the user ended the
// connection they were issued under.
func (r *RefreshTokenRepo) FamilyActive(ctx context.Context, familyID string) (bool, error) {
	if familyID == "" {
		return false, nil
	}
	// Not live(): while a refresh is in flight the old token is already spent
	// and the new one not yet stored, and a request arriving in between must
	// not be turned away. Ending a connection revokes every token of the
	// family, spent ones included, so "unrevoked and unexpired" is exact.
	count, err := r.collection.CountDocuments(ctx,
		bson.M{"family_id": familyID, "revoked": bson.M{"$ne": true}, "expires_at": bson.M{"$gt": time.Now()}},
		options.Count().SetLimit(1))
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// Consume marks the presented token as used and returns it, exactly once. The
// claim is a single atomic update, so of two requests racing with the same
// token only one can win.
//
// A token that was already used is the signature of a stolen token: the thief
// and the client both hold it and one of them got there first. Outside the
// retry grace the whole family is revoked, which signs both of them out and
// sends the real user back through consent.
func (r *RefreshTokenRepo) Consume(ctx context.Context, secret, clientID string) (*RefreshToken, error) {
	if secret == "" {
		return nil, ErrRefreshTokenInvalid
	}

	hash := utils.HashSecret(secret)
	now := time.Now()

	token := &RefreshToken{}
	err := r.collection.FindOneAndUpdate(ctx,
		bson.M{
			"token_hash": hash,
			"client_id":  clientID,
			"used_at":    bson.M{"$exists": false},
			"revoked":    bson.M{"$ne": true},
			"expires_at": bson.M{"$gt": now},
		},
		bson.M{"$set": bson.M{"used_at": now}},
	).Decode(token)
	if err == nil {
		return token, nil
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		return nil, err
	}

	// Not claimable. Find out whether it is a replay.
	seen := &RefreshToken{}
	if err := r.collection.FindOne(ctx, bson.M{"token_hash": hash}).Decode(seen); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, ErrRefreshTokenInvalid
		}
		return nil, err
	}
	if seen.UsedAt != nil && now.Sub(*seen.UsedAt) > refreshReuseGrace {
		if err := r.RevokeFamily(ctx, seen.FamilyID); err != nil {
			return nil, err
		}
	}

	return nil, ErrRefreshTokenInvalid
}

// ClientsInUse reports which of clientIDs still hold a refresh token that has
// not expired, revoked or not.
func (r *RefreshTokenRepo) ClientsInUse(ctx context.Context, clientIDs []string) (map[string]bool, error) {
	inUse := map[string]bool{}
	if len(clientIDs) == 0 {
		return inUse, nil
	}
	values, err := r.collection.Distinct(ctx, "client_id", bson.M{
		"client_id":  bson.M{"$in": clientIDs},
		"expires_at": bson.M{"$gt": time.Now()},
	})
	if err != nil {
		return nil, err
	}
	for _, value := range values {
		if id, ok := value.(string); ok {
			inUse[id] = true
		}
	}
	return inUse, nil
}

// RevokeFamily revokes every token descended from one authorization.
func (r *RefreshTokenRepo) RevokeFamily(ctx context.Context, familyID string) error {
	if familyID == "" {
		return nil
	}
	_, err := r.collection.UpdateMany(ctx,
		bson.M{"family_id": familyID},
		bson.M{"$set": bson.M{"revoked": true}})
	return err
}

// RevokeUser revokes every refresh token of an account, for when its
// credentials change or it is disabled.
func (r *RefreshTokenRepo) RevokeUser(ctx context.Context, userPrn string) error {
	if userPrn == "" {
		return nil
	}
	_, err := r.collection.UpdateMany(ctx,
		bson.M{"user_prn": userPrn},
		bson.M{"$set": bson.M{"revoked": true}})
	return err
}
