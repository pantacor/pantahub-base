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
package devices

import (
	"context"
	"log"
	"time"

	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

const (
	secretMigrationBatch = 500
	secretMigrationPause = 250 * time.Millisecond
)

// RunSecretMigration hashes every device secret still stored only in
// plaintext, in small throttled batches so a fleet of millions never puts a
// spike on the database. It is idempotent and safe to run concurrently on
// every replica: each update is predicated on the row still being
// unmigrated, so a batch two replicas pick up at once is only applied once.
// The plaintext field is kept until PANTAHUB_PURGE_PLAINTEXT_SECRETS=true,
// after which a second batched pass removes it.
func RunSecretMigration(ctx context.Context, col *mongo.Collection) {
	hashed, err := hashPlaintextDeviceSecrets(ctx, col)
	if err != nil {
		log.Printf("devices: secret migration stopped after %d rows: %v", hashed, err)
		return
	}
	if hashed > 0 {
		log.Printf("devices: hashed %d legacy plaintext device secrets", hashed)
	}

	if utils.GetEnv(utils.EnvPantahubPurgePlaintextSecrets) != "true" {
		return
	}
	purged, err := purgePlaintextDeviceSecrets(ctx, col)
	if err != nil {
		log.Printf("devices: plaintext secret purge stopped after %d rows: %v", purged, err)
		return
	}
	if purged > 0 {
		log.Printf("devices: purged %d legacy plaintext device secrets", purged)
	}
}

func hashPlaintextDeviceSecrets(ctx context.Context, col *mongo.Collection) (int64, error) {
	var total int64
	for {
		cursor, err := col.Find(ctx,
			bson.M{"secret": bson.M{"$exists": true, "$type": "string", "$ne": ""}, "secret_hash": bson.M{"$exists": false}},
			options.Find().SetProjection(bson.M{"_id": 1, "secret": 1}).SetLimit(secretMigrationBatch))
		if err != nil {
			return total, err
		}

		var batch int64
		for cursor.Next(ctx) {
			var legacy struct {
				ID     primitive.ObjectID `bson:"_id"`
				Secret string             `bson:"secret"`
			}
			if err := cursor.Decode(&legacy); err != nil {
				cursor.Close(ctx)
				return total, err
			}
			res, err := col.UpdateOne(ctx,
				bson.M{"_id": legacy.ID, "secret": legacy.Secret, "secret_hash": bson.M{"$exists": false}},
				bson.M{"$set": bson.M{"secret_hash": utils.HashSecret(legacy.Secret)}})
			if err != nil {
				cursor.Close(ctx)
				return total, err
			}
			batch++
			total += res.ModifiedCount
		}
		err = cursor.Err()
		cursor.Close(ctx)
		if err != nil {
			return total, err
		}
		if batch < secretMigrationBatch {
			return total, nil
		}
		if !sleepCtx(ctx, secretMigrationPause) {
			return total, ctx.Err()
		}
	}
}

func purgePlaintextDeviceSecrets(ctx context.Context, col *mongo.Collection) (int64, error) {
	var total int64
	for {
		cursor, err := col.Find(ctx,
			bson.M{"secret": bson.M{"$exists": true}, "secret_hash": bson.M{"$exists": true, "$ne": ""}},
			options.Find().SetProjection(bson.M{"_id": 1}).SetLimit(secretMigrationBatch))
		if err != nil {
			return total, err
		}
		ids := []primitive.ObjectID{}
		for cursor.Next(ctx) {
			var row struct {
				ID primitive.ObjectID `bson:"_id"`
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
			bson.M{"_id": bson.M{"$in": ids}, "secret_hash": bson.M{"$exists": true, "$ne": ""}},
			bson.M{"$unset": bson.M{"secret": ""}})
		if err != nil {
			return total, err
		}
		total += res.ModifiedCount
		if len(ids) < secretMigrationBatch {
			return total, nil
		}
		if !sleepCtx(ctx, secretMigrationPause) {
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
