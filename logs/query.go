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
	"errors"
	"time"
)

// Query selects log entries for GetLogs. Owner is mandatory: it is what keeps
// one account from reading another account's logs, so it must come from the
// authenticated identity and never from caller supplied input.
type Query struct {
	Owner    string
	Device   string // comma separated device PRNs, as ParseDeviceString returns
	Level    string
	Source   string
	Rev      string
	Platform string

	Start  int64
	Page   int64
	Before *time.Time
	After  *time.Time
	Sort   Sorts
}

// GetLogs is the read counterpart of PostLogs: it lets a transport other than
// the REST handler (the MCP server) query the same store GET /logs serves,
// with the same page cap. Like PostLogs it must be called on the App built by
// New, because only that one has a registered backend.
func (a *App) GetLogs(ctx context.Context, q Query) (*Pager, error) {
	if q.Owner == "" {
		return nil, errors.New("logs: query without owner")
	}

	if q.Page <= 0 {
		q.Page = 50
	}
	if q.Page > maxPagination {
		q.Page = maxPagination
	}
	if q.Start < 0 {
		q.Start = 0
	}

	filter := &Entry{
		Owner:     q.Owner,
		Device:    q.Device,
		LogLevel:  q.Level,
		LogSource: q.Source,
		LogRev:    q.Rev,
		LogPlat:   q.Platform,
	}

	return a.backend.getLogs(ctx, q.Start, q.Page, q.Before, q.After, filter, q.Sort, nil, false)
}
