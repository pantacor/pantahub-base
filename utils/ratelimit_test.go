// Copyright (c) 2017-2026 Pantacor Ltd.
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

package utils

import (
	"net/http"
	"testing"
)

func TestIPRateLimiterBurstThenDeny(t *testing.T) {
	l := NewIPRateLimiter(0, 3)
	for i := 0; i < 3; i++ {
		if !l.Allow("a") {
			t.Fatalf("request %d within burst must be allowed", i)
		}
	}
	if l.Allow("a") {
		t.Fatal("request beyond burst must be denied")
	}
	if !l.Allow("b") {
		t.Fatal("another client must have its own bucket")
	}
}

func TestClientIP(t *testing.T) {
	r, _ := http.NewRequest("GET", "/", nil)
	r.RemoteAddr = "10.0.0.5:4444"
	if got := ClientIP(r); got != "10.0.0.5" {
		t.Fatalf("RemoteAddr host expected, got %q", got)
	}
	r.Header.Set("X-Forwarded-For", " 203.0.113.9 , 10.0.0.1")
	if got := ClientIP(r); got != "203.0.113.9" {
		t.Fatalf("first X-Forwarded-For entry expected, got %q", got)
	}
}
