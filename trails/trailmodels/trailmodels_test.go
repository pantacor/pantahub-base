// Copyright 2026  Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package trailmodels

import (
	"testing"
	"time"
)

func TestFillLastSeen(t *testing.T) {
	timestamp := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	lastSeen := time.Date(2026, 8, 10, 9, 30, 0, 0, time.UTC)

	cases := []struct {
		name     string
		lastSeen time.Time
		want     time.Time
	}{
		{"zero time falls back to timestamp", time.Time{}, timestamp},
		{"unix epoch falls back to timestamp", time.Unix(0, 0).UTC(), timestamp},
		{"real value is kept", lastSeen, lastSeen},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := TrailSummary{Timestamp: timestamp, LastSeen: c.lastSeen}
			s.FillLastSeen()
			if !s.LastSeen.Equal(c.want) {
				t.Errorf("LastSeen = %v, want %v", s.LastSeen, c.want)
			}
		})
	}
}
