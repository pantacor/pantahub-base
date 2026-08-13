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
)

// PostLogs stores entries using the configured backend, for callers that ingest
// logs outside the REST API — currently the MQTT message plane. Entries reach
// the same store whichever transport carried them, so a device that publishes
// logs over MQTT is queryable through GET /logs exactly like one that posts them.
//
// It must be called on the App built by New, not on a separately constructed
// one. Backend registration is what marks an elastic backend usable (it sets
// the per-instance "works" flag, not just the server-side index template), and
// New is what registers it. A second App built from the same environment would
// look correctly configured and silently drop every entry — visible only as a
// server-side log line, because a QoS 1 PUBACK tells the device the broker
// accepted the packet, not that the write succeeded.
//
// Callers are responsible for filling in the fields the REST handler would set
// from the request identity — Device, Owner and TimeCreated — because those
// come from the authenticated session rather than from the payload.
func (a *App) PostLogs(ctx context.Context, entries []Entry) error {
	if len(entries) == 0 {
		return nil
	}

	return a.backend.postLogs(ctx, entries, false)
}
