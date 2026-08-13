//
// Copyright 2026 Pantacor Ltd.
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

package logs

import (
	"context"

	"go.mongodb.org/mongo-driver/mongo"
)

// NewIngest builds a logs App that can only store entries, for callers that
// ingest logs outside the REST API — currently the MQTT message plane.
//
// Backend selection is identical to New, so entries reach the same store
// whichever transport carried them: a device that publishes logs over MQTT is
// queryable through GET /logs exactly like one that posts them.
//
// Unlike New it neither builds the REST API nor registers the backend, because
// the REST App created earlier in base.DoInit already owns registration, and it
// returns errors instead of exiting: the message plane is optional, and a
// logging misconfiguration must not take the API process down with it.
func NewIngest(mongoClient *mongo.Client) (*App, error) {
	app := new(App)
	app.mongoClient = mongoClient

	backend, err := NewElasticLogger()
	if err != nil {
		return nil, err
	}

	if backend == nil {
		backend, err = NewMgoLogger(mongoClient)
		if err != nil {
			return nil, err
		}
	}

	app.backend = backend

	return app, nil
}

// PostLogs stores entries using the configured backend. Callers are responsible
// for filling in the fields the REST handler would set from the request
// identity — Device, Owner and TimeCreated — because those come from the
// authenticated session rather than from the payload.
func (a *App) PostLogs(ctx context.Context, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}

	return a.backend.postLogs(ctx, entries, false)
}
