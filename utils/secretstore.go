// Copyright 2026 Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.

// Package-level helpers for machine secrets stored at rest (personal access
// tokens, OAuth client secrets, device secrets). One convention across all of
// them: the plaintext lives in the "secret" field only until it has been
// hashed into "secret_hash" (hex SHA-256); a fast hash is deliberate — these
// secrets are CSPRNG-generated, so a slow KDF adds nothing and they are
// verified on every request that presents them.
//
// The rollout is two-phase so a running fleet is never locked out:
//  1. MigrateSecrets adds secret_hash to every legacy row, keeping the
//     plaintext so replicas on the previous build keep authenticating.
//  2. once every replica verifies by hash, PurgeSecrets drops the plaintext.
//
// VerifyStoredSecret bridges the two phases for id-based lookups: it matches a
// hashed row, falls back to a plaintext row, and reports the hash to persist
// so that row upgrades itself on first use.
package utils

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// SecretPlainField / SecretHashField are the shared document fields.
const (
	SecretPlainField = "secret"
	SecretHashField  = "secret_hash"

	defaultSecretBatch = 500
	secretBatchPause   = 200 * time.Millisecond
)

// HashSecret returns the hex SHA-256 digest of a machine secret — the form in
// which secrets are stored at rest.
func HashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

// SecretHashMatches reports whether presented hashes to storedHash, comparing
// in constant time. Empty inputs never match.
func SecretHashMatches(storedHash, presented string) bool {
	if storedHash == "" || presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(HashSecret(presented)), []byte(storedHash)) == 1
}

// VerifyStoredSecret checks a presented secret against a stored record that
// may hold a hash, a legacy plaintext, or both. It returns whether the secret
// is valid and, when it matched a not-yet-migrated plaintext row, the hash the
// caller should persist so the row upgrades itself (empty when nothing to do).
func VerifyStoredSecret(storedHash, storedPlain, presented string) (ok bool, upgrade string) {
	if presented == "" {
		return false, ""
	}
	if storedHash != "" {
		return SecretHashMatches(storedHash, presented), ""
	}
	if storedPlain != "" && subtle.ConstantTimeCompare([]byte(storedPlain), []byte(presented)) == 1 {
		return true, HashSecret(presented)
	}
	return false, ""
}

// MigrateSecrets hashes every row in col that still holds a plaintext secret
// but no secret_hash, in throttled batches so a large collection never spikes
// the database. The plaintext is kept (PurgeSecrets removes it later). It is
// idempotent and safe to run concurrently on every replica: each update is
// predicated on the row still being unmigrated. batchSize <= 0 uses the
// default. Returns the number of rows migrated.
func MigrateSecrets(ctx context.Context, col *mongo.Collection, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = defaultSecretBatch
	}
	filter := bson.M{
		SecretPlainField: bson.M{"$exists": true, "$type": "string", "$ne": ""},
		SecretHashField:  bson.M{"$exists": false},
	}
	proj := options.Find().SetProjection(bson.M{"_id": 1, SecretPlainField: 1}).SetLimit(int64(batchSize))

	var total int64
	for {
		cursor, err := col.Find(ctx, filter, proj)
		if err != nil {
			return total, err
		}
		var seen int
		for cursor.Next(ctx) {
			var row struct {
				ID     interface{} `bson:"_id"`
				Secret string      `bson:"secret"`
			}
			if err := cursor.Decode(&row); err != nil {
				cursor.Close(ctx)
				return total, err
			}
			seen++
			res, err := col.UpdateOne(ctx,
				bson.M{"_id": row.ID, SecretPlainField: row.Secret, SecretHashField: bson.M{"$exists": false}},
				bson.M{"$set": bson.M{SecretHashField: HashSecret(row.Secret)}})
			if err != nil {
				cursor.Close(ctx)
				return total, err
			}
			total += res.ModifiedCount
		}
		err = cursor.Err()
		cursor.Close(ctx)
		if err != nil {
			return total, err
		}
		if seen < batchSize {
			return total, nil
		}
		if !sleepCtx(ctx, secretBatchPause) {
			return total, ctx.Err()
		}
	}
}

// PurgeSecrets drops the legacy plaintext secret from every row in col that
// already carries a secret_hash, in throttled batches. Run only once no
// replica needs the plaintext any more. Returns the number of rows purged.
func PurgeSecrets(ctx context.Context, col *mongo.Collection, batchSize int) (int64, error) {
	if batchSize <= 0 {
		batchSize = defaultSecretBatch
	}
	filter := bson.M{
		SecretPlainField: bson.M{"$exists": true},
		SecretHashField:  bson.M{"$exists": true, "$ne": ""},
	}
	proj := options.Find().SetProjection(bson.M{"_id": 1}).SetLimit(int64(batchSize))

	var total int64
	for {
		cursor, err := col.Find(ctx, filter, proj)
		if err != nil {
			return total, err
		}
		ids := make([]interface{}, 0, batchSize)
		for cursor.Next(ctx) {
			var row struct {
				ID interface{} `bson:"_id"`
			}
			if err := cursor.Decode(&row); err != nil {
				cursor.Close(ctx)
				return total, err
			}
			ids = append(ids, row.ID)
		}
		err = cursor.Err()
		cursor.Close(ctx)
		if err != nil {
			return total, err
		}
		if len(ids) == 0 {
			return total, nil
		}
		res, err := col.UpdateMany(ctx,
			bson.M{"_id": bson.M{"$in": ids}, SecretHashField: bson.M{"$exists": true, "$ne": ""}},
			bson.M{"$unset": bson.M{SecretPlainField: ""}})
		if err != nil {
			return total, err
		}
		total += res.ModifiedCount
		if len(ids) < batchSize {
			return total, nil
		}
		if !sleepCtx(ctx, secretBatchPause) {
			return total, ctx.Err()
		}
	}
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}
