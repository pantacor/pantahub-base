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
	"errors"
	"fmt"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	// ErrMFAReplayed the presented proof (jti or TOTP step) was already used
	ErrMFAReplayed = errors.New("mfa proof already used")
)

// MFARepo manages MFASettings and used MFA-pending token ids in MongoDB
type MFARepo struct {
	mongoClient *mongo.Client
	settings    *mongo.Collection
	usedJTIs    *mongo.Collection
}

// NewMFARepo creates an MFARepo bound to the given mongo client. It uses
// utils.MongoDb so it follows the test database when tests switch it.
func NewMFARepo(mongoClient *mongo.Client) *MFARepo {
	db := mongoClient.Database(utils.MongoDb)
	return &MFARepo{
		mongoClient: mongoClient,
		settings:    db.Collection(MFASettingsCollection),
		usedJTIs:    db.Collection(MFAUsedJTICollection),
	}
}

// SetIndexes creates the unique owner index and the used-jti TTL index
func (r *MFARepo) SetIndexes(ctx context.Context) error {
	t := true
	var zero int32 = 0

	_, err := r.settings.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "owner", Value: 1}},
		Options: &options.IndexOptions{Unique: &t},
	})
	if err != nil {
		return fmt.Errorf("error setting up index for %s: %s", MFASettingsCollection, err.Error())
	}

	sparse := true
	_, err = r.settings.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "user_handle", Value: 1}},
		Options: &options.IndexOptions{Unique: &t, Sparse: &sparse},
	})
	if err != nil {
		return fmt.Errorf("error setting up user_handle index for %s: %s", MFASettingsCollection, err.Error())
	}

	_, err = r.usedJTIs.Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "expires_at", Value: 1}},
		Options: &options.IndexOptions{ExpireAfterSeconds: &zero},
	})
	if err != nil {
		return fmt.Errorf("error setting up index for %s: %s", MFAUsedJTICollection, err.Error())
	}

	return nil
}

// GetByUserHandle resolves MFA settings from a WebAuthn user handle asserted
// during a passkey (discoverable) login. Returns (nil, nil) when unknown.
func (r *MFARepo) GetByUserHandle(ctx context.Context, userHandle []byte) (*MFASettings, error) {
	s := &MFASettings{}
	err := r.settings.FindOne(ctx, bson.M{"user_handle": userHandle}).Decode(s)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// SetEnabled flips the MFA master switch for an account (used when factors
// are added/removed outside the TOTP lifecycle)
func (r *MFARepo) SetEnabled(ctx context.Context, ownerPrn string, enabled bool) error {
	update := bson.M{"$set": bson.M{
		"enabled":       enabled,
		"time-modified": time.Now(),
	}}
	if !enabled {
		update["$set"].(bson.M)["recovery_codes"] = []RecoveryCode{}
		update["$set"].(bson.M)["failed_attempts"] = 0
	}

	res, err := r.settings.UpdateOne(ctx, bson.M{"owner": ownerPrn}, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// GetByOwner loads the MFA settings for an account PRN. Returns (nil, nil)
// when the account has no MFA settings yet.
func (r *MFARepo) GetByOwner(ctx context.Context, ownerPrn string) (*MFASettings, error) {
	s := &MFASettings{}
	err := r.settings.FindOne(ctx, bson.M{"owner": ownerPrn}).Decode(s)
	if err == mongo.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Upsert writes the full settings doc for its owner
func (r *MFARepo) Upsert(ctx context.Context, s *MFASettings) error {
	now := time.Now()
	if s.ID.IsZero() {
		s.ID = primitive.NewObjectID()
		s.Prn = utils.IDGetPrn(s.ID, "mfa-settings")
		s.TimeCreated = now
	}
	s.TimeModified = now

	upsert := true
	_, err := r.settings.UpdateOne(ctx,
		bson.M{"owner": s.Owner},
		bson.M{"$set": s},
		&options.UpdateOptions{Upsert: &upsert},
	)
	return err
}

// ConfirmTOTP marks the pending TOTP factor as confirmed, enables MFA and
// stores the initial recovery code set
func (r *MFARepo) ConfirmTOTP(ctx context.Context, ownerPrn string, lastUsedStep int64, codes []RecoveryCode) error {
	now := time.Now()
	res, err := r.settings.UpdateOne(ctx,
		bson.M{"owner": ownerPrn, "totp": bson.M{"$ne": nil}, "totp.confirmed": false},
		bson.M{"$set": bson.M{
			"enabled":             true,
			"totp.confirmed":      true,
			"totp.confirmed_at":   now,
			"totp.last_used_step": lastUsedStep,
			"recovery_codes":      codes,
			"failed_attempts":     0,
			"time-modified":       now,
		}, "$unset": bson.M{"locked_until": ""}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// UseTOTPStep atomically records the accepted TOTP time-step. It only
// succeeds when step is strictly greater than the stored last_used_step,
// which rejects replayed and older codes; on replay it returns ErrMFAReplayed.
func (r *MFARepo) UseTOTPStep(ctx context.Context, ownerPrn string, step int64) error {
	res, err := r.settings.UpdateOne(ctx,
		bson.M{
			"owner":               ownerPrn,
			"totp.confirmed":      true,
			"totp.last_used_step": bson.M{"$lt": step},
		},
		bson.M{"$set": bson.M{
			"totp.last_used_step": step,
			"failed_attempts":     0,
			"time-modified":       time.Now(),
		}, "$unset": bson.M{"locked_until": ""}},
	)
	if err != nil {
		return err
	}
	if res.ModifiedCount == 0 {
		return ErrMFAReplayed
	}
	return nil
}

// UseRecoveryCode atomically marks the recovery code at index used. Fails
// with ErrMFAReplayed if it was already consumed.
func (r *MFARepo) UseRecoveryCode(ctx context.Context, ownerPrn string, index int) error {
	now := time.Now()
	field := fmt.Sprintf("recovery_codes.%d.used_at", index)
	res, err := r.settings.UpdateOne(ctx,
		bson.M{"owner": ownerPrn, field: nil},
		bson.M{"$set": bson.M{
			field:             now,
			"failed_attempts": 0,
			"time-modified":   now,
		}, "$unset": bson.M{"locked_until": ""}},
	)
	if err != nil {
		return err
	}
	if res.ModifiedCount == 0 {
		return ErrMFAReplayed
	}
	return nil
}

// SetRecoveryCodes replaces the recovery code set (regeneration)
func (r *MFARepo) SetRecoveryCodes(ctx context.Context, ownerPrn string, codes []RecoveryCode) error {
	res, err := r.settings.UpdateOne(ctx,
		bson.M{"owner": ownerPrn, "enabled": true},
		bson.M{"$set": bson.M{
			"recovery_codes": codes,
			"time-modified":  time.Now(),
		}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// RemoveTOTP removes the TOTP factor. When the account is left with no other
// confirmed second factor (hasOtherFactors == false) MFA is disabled and the
// recovery codes are invalidated.
func (r *MFARepo) RemoveTOTP(ctx context.Context, ownerPrn string, hasOtherFactors bool) error {
	update := bson.M{
		"$unset": bson.M{"totp": ""},
		"$set":   bson.M{"time-modified": time.Now()},
	}
	if !hasOtherFactors {
		update["$set"] = bson.M{
			"enabled":         false,
			"recovery_codes":  []RecoveryCode{},
			"failed_attempts": 0,
			"time-modified":   time.Now(),
		}
	}

	res, err := r.settings.UpdateOne(ctx, bson.M{"owner": ownerPrn}, update)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

// RegisterFailure counts a failed second-factor proof and locks the account's
// MFA step for lockDuration once maxFailures consecutive failures are
// reached. Returns whether the account is now locked.
func (r *MFARepo) RegisterFailure(ctx context.Context, ownerPrn string, maxFailures int, lockDuration time.Duration) (bool, error) {
	s := &MFASettings{}
	f := false
	after := options.After
	err := r.settings.FindOneAndUpdate(ctx,
		bson.M{"owner": ownerPrn},
		bson.M{
			"$inc": bson.M{"failed_attempts": 1},
			"$set": bson.M{"time-modified": time.Now()},
		},
		&options.FindOneAndUpdateOptions{Upsert: &f, ReturnDocument: &after},
	).Decode(s)
	if err != nil {
		return false, err
	}

	if s.FailedAttempts >= maxFailures {
		lockedUntil := time.Now().Add(lockDuration)
		_, err = r.settings.UpdateOne(ctx,
			bson.M{"owner": ownerPrn},
			bson.M{"$set": bson.M{"locked_until": lockedUntil, "failed_attempts": 0}},
		)
		if err != nil {
			return false, err
		}
		return true, nil
	}

	return false, nil
}

// ConsumeJTI records a single-use MFA-pending token id. A second consumption
// of the same jti fails with ErrMFAReplayed (unique _id).
func (r *MFARepo) ConsumeJTI(ctx context.Context, jti string, expiresAt time.Time) error {
	_, err := r.usedJTIs.InsertOne(ctx, bson.M{
		"_id":        jti,
		"expires_at": expiresAt.Add(time.Minute), // keep past token expiry
	})
	if mongo.IsDuplicateKeyError(err) {
		return ErrMFAReplayed
	}
	return err
}
