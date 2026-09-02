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
package utils

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// IPRateLimiter is a per-client token bucket kept in memory: each client
// gets burst tokens, refilled at rate tokens per second. It is per replica
// (not shared across pods), which is enough to blunt cheap unauthenticated
// amplification without any extra infrastructure.
type IPRateLimiter struct {
	rate  float64
	burst float64

	mu      sync.Mutex
	buckets map[string]*ipBucket
	lastGC  time.Time
}

type ipBucket struct {
	tokens float64
	last   time.Time
}

// NewIPRateLimiter allows burst requests at once and rate requests per
// second sustained, per client key.
func NewIPRateLimiter(rate float64, burst int) *IPRateLimiter {
	return &IPRateLimiter{
		rate:    rate,
		burst:   float64(burst),
		buckets: map[string]*ipBucket{},
		lastGC:  time.Now(),
	}
}

// Allow reports whether the client identified by key may proceed now.
func (l *IPRateLimiter) Allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if now.Sub(l.lastGC) > 5*time.Minute {
		for k, b := range l.buckets {
			if now.Sub(b.last) > 5*time.Minute {
				delete(l.buckets, k)
			}
		}
		l.lastGC = now
	}

	b, ok := l.buckets[key]
	if !ok {
		b = &ipBucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// ClientIP returns the originating client address of a request. The ingress
// APPENDS the peer address to any X-Forwarded-For the client sent, so the
// rightmost entry is the one the ingress observed; the leftmost is
// client-forgeable, and keying rate limits on it would let a client rotate
// its identity per request.
func ClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if last := strings.TrimSpace(parts[len(parts)-1]); last != "" {
			return last
		}
	}
	if xr := strings.TrimSpace(r.Header.Get("X-Real-Ip")); xr != "" {
		return xr
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
