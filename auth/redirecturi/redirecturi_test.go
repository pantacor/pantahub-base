// Copyright 2016-2026  Pantacor Ltd.
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

package redirecturi

import "testing"

// appCallbacks are the callback URLs registered on a third party OAuth
// application. Only the code and PKCE flows validate against these; the social
// login flow is pinned to our own web interface origin instead.
var appCallbacks = []string{
	"http://localhost:9090/callback",
	"https://app.example.com/oauth/callback",
	"https://staging.app.example.com/oauth/callback",
}

// originCallbacks are registered as bare origins, which is how the existing
// registrations are written: the client then redirects to a path beneath them.
var originCallbacks = []string{
	"http://localhost:9090",
	"https://app.example.com",
}

// A registration written as a bare origin covers the paths beneath it, so
// clients that register an origin and redirect to a path keep working.
func TestMatchOriginRegistrationCoversPathsBeneathIt(t *testing.T) {
	accepted := []string{
		"https://app.example.com/auth/oauth/callback/somefqdn",
		"https://app.example.com/oauth/callback",
		"https://app.example.com/",
		"https://app.example.com",
		// Loopback keeps the port exemption on top of the path rule.
		"http://127.0.0.1:54321/callback",
	}

	for _, candidate := range accepted {
		if !Match(originCallbacks, candidate) {
			t.Errorf("Match rejected a path beneath a registered origin: %q", candidate)
		}
	}

	rejected := []string{
		// Still compared as components, so a suffix is not a match.
		"https://app.example.com.evil.example/oauth/callback",
		"https://attacker.example/oauth/callback",
		"http://app.example.com/oauth/callback",
	}

	for _, candidate := range rejected {
		if Match(originCallbacks, candidate) {
			t.Errorf("Match accepted a foreign host against a registered origin: %q", candidate)
		}
	}
}

// A registered path confines the target to that subtree, and traversal must not
// escape it.
func TestMatchPathPrefixIsBounded(t *testing.T) {
	accepted := []string{
		"https://app.example.com/oauth/callback",
		"https://app.example.com/oauth/callback/",
		"https://app.example.com/oauth/callback/extra",
	}

	for _, candidate := range accepted {
		if !Match(appCallbacks, candidate) {
			t.Errorf("Match rejected a path within the registered subtree: %q", candidate)
		}
	}

	rejected := []string{
		// Sibling path sharing a textual prefix.
		"https://app.example.com/oauth/callback-evil",
		// A different subtree entirely.
		"https://app.example.com/elsewhere",
		// Traversal back out of the registered subtree.
		"https://app.example.com/oauth/callback/../../evil",
		"https://app.example.com/oauth/callback/..%2f..%2fevil",
	}

	for _, candidate := range rejected {
		if Match(appCallbacks, candidate) {
			t.Errorf("Match accepted a path outside the registered subtree: %q", candidate)
		}
	}
}

func TestMatchRejectsUnregisteredHosts(t *testing.T) {
	cases := []string{
		// A wholly unrelated host.
		"https://attacker.example",
		"https://attacker.example/",
		// Suffix confusion against a registered host.
		"https://app.example.com.evil.example/oauth/callback",
		"https://app.example.com.evil.example",
		// Prefix confusion, which the previous strings.HasPrefix check allowed.
		"https://app.example.com/oauth/callback/../../evil",
		// Credentials in the authority disguising the real host.
		"https://app.example.com@evil.example/oauth/callback",
		// Scheme downgrade against a registered https callback.
		"http://app.example.com/oauth/callback",
		// Script bearing schemes.
		"javascript:alert(document.domain)",
		"data:text/html,<script>alert(1)</script>",
		// Unparseable or empty.
		"",
		"://",
		"not a url",
	}

	for _, candidate := range cases {
		if Match(appCallbacks, candidate) {
			t.Errorf("Match accepted unregistered redirect_uri %q", candidate)
		}
	}
}

func TestMatchAcceptsExactRegisteredCallbacks(t *testing.T) {
	cases := []string{
		"https://app.example.com/oauth/callback",
		"https://staging.app.example.com/oauth/callback",
		// The default port is equivalent to omitting it.
		"https://app.example.com:443/oauth/callback",
		// Scheme and host compare case insensitively.
		"HTTPS://App.Example.Com/oauth/callback",
	}

	for _, candidate := range cases {
		if !Match(appCallbacks, candidate) {
			t.Errorf("Match rejected registered redirect_uri %q", candidate)
		}
	}
}

// Native clients bind an ephemeral loopback port, so the port is excluded from
// the comparison per RFC 8252 section 7.3. pvr and fleetcli both do this.
func TestMatchLoopbackIgnoresPort(t *testing.T) {
	accepted := []string{
		"http://127.0.0.1:54321/callback",
		"http://127.0.0.1:9090/callback",
		"http://localhost:41234/callback",
		"http://[::1]:41234/callback",
	}

	for _, candidate := range accepted {
		if !Match(appCallbacks, candidate) {
			t.Errorf("Match rejected loopback redirect_uri %q", candidate)
		}
	}

	rejected := []string{
		// The path still has to match.
		"http://127.0.0.1:54321/not-the-callback",
		// The exemption is for loopback only, not for arbitrary hosts.
		"http://evil.example:54321/callback",
		// Nor for hosts that merely look like loopback.
		"http://127.0.0.1.evil.example:54321/callback",
		"http://localhost.evil.example/callback",
	}

	for _, candidate := range rejected {
		if Match(appCallbacks, candidate) {
			t.Errorf("Match accepted non loopback redirect_uri %q", candidate)
		}
	}
}

func TestMatchRejectsFragments(t *testing.T) {
	// A fragment on the candidate would collide with the #token= we append.
	if Match(appCallbacks, "https://app.example.com/oauth/callback#token=stolen") {
		t.Error("Match accepted a redirect_uri carrying a fragment")
	}
}

func TestValidateFailsClosedWithoutRegisteredCallbacks(t *testing.T) {
	err := Validate(nil, "https://app.example.com/oauth/callback", AuditContext{})
	if err != ErrNoneRegistered {
		t.Errorf("expected ErrNoneRegistered for an app with no callbacks, got %v", err)
	}

	err = Validate([]string{}, "https://anything", AuditContext{})
	if err != ErrNoneRegistered {
		t.Errorf("expected ErrNoneRegistered for an empty callback list, got %v", err)
	}
}

func TestValidateReturnsNotRegistered(t *testing.T) {
	err := Validate(appCallbacks, "https://attacker.example", AuditContext{})
	if err != ErrNotRegistered {
		t.Errorf("expected ErrNotRegistered, got %v", err)
	}
}

// A registered entry that does not parse must be skipped, never treated as a
// wildcard that lets everything through.
func TestMatchSkipsUnparseableRegisteredEntries(t *testing.T) {
	registered := []string{"", "://broken", "javascript:alert(1)"}

	if Match(registered, "https://attacker.example") {
		t.Error("Match treated an unparseable registered entry as a wildcard")
	}
}

// The social login flow returns to our own web interface, so the whole origin
// is trusted but nothing outside it is.
func TestMatchOriginAllowsAnyPathOnTheOrigin(t *testing.T) {
	origins := []string{"https://hub.example.com"}

	accepted := []string{
		"https://hub.example.com/login",
		"https://hub.example.com/",
		"https://hub.example.com/some/other/page",
		"https://hub.example.com:443/login",
	}

	for _, candidate := range accepted {
		if !MatchOrigin(origins, candidate) {
			t.Errorf("MatchOrigin rejected same origin redirect_uri %q", candidate)
		}
	}

	rejected := []string{
		"https://attacker.example",
		"https://attacker.example/login",
		"https://hub.example.com.evil.example/login",
		"http://hub.example.com/login",
		"https://hub.example.com@evil.example/login",
		"https://evil.example/login?x=https://hub.example.com",
	}

	for _, candidate := range rejected {
		if MatchOrigin(origins, candidate) {
			t.Errorf("MatchOrigin accepted foreign redirect_uri %q", candidate)
		}
	}
}

// The social login callback hands back a signed in user token in the fragment,
// so it must return to our own web interface and nowhere else. In particular a
// callback registered on a legitimate third party application is still not an
// acceptable destination for this flow.
func TestSocialLoginOriginRejectsRegisteredAppCallbacks(t *testing.T) {
	origins := []string{"https://hub.example.com"}

	for _, candidate := range appCallbacks {
		if MatchOrigin(origins, candidate) {
			t.Errorf("social login origin check accepted application callback %q", candidate)
		}
	}
}

func TestValidateOriginFailsClosedWithoutOrigins(t *testing.T) {
	err := ValidateOrigin(nil, "https://hub.example.com/login", AuditContext{})
	if err != ErrNoneRegistered {
		t.Errorf("expected ErrNoneRegistered when no origins are configured, got %v", err)
	}
}
