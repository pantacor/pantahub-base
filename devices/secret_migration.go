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

	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
)

// RunSecretMigration hashes legacy plaintext device secrets in throttled
// batches (utils.MigrateSecrets) and, once PANTAHUB_PURGE_PLAINTEXT_SECRETS is
// set, drops the plaintext (utils.PurgeSecrets). Meant to run in the
// background at startup; devices that log in meanwhile upgrade themselves via
// DeviceAuth. Safe to run on every replica.
func RunSecretMigration(ctx context.Context, col *mongo.Collection) {
	if n, err := utils.MigrateSecrets(ctx, col, 0); err != nil {
		log.Printf("devices: secret migration stopped after %d rows: %v", n, err)
		return
	} else if n > 0 {
		log.Printf("devices: hashed %d legacy plaintext device secrets", n)
	}

	if utils.GetEnv(utils.EnvPantahubPurgePlaintextSecrets) != "true" {
		return
	}
	if n, err := utils.PurgeSecrets(ctx, col, 0); err != nil {
		log.Printf("devices: plaintext secret purge stopped after %d rows: %v", n, err)
		return
	} else if n > 0 {
		log.Printf("devices: purged %d legacy plaintext device secrets", n)
	}
}
