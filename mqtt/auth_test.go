//
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
//

package mqtt

import (
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// userScope is the space-separated scope string a login/API token carries.
const (
	scopeAll      = "prn:pantahub.com:apis:/base/all"
	scopeReadOnly = "prn:pantahub.com:apis:/base/all.readonly"
)

var discardLogger = slog.New(slog.NewTextHandler(io.Discard, nil))

func init() {
	// mqttReadDeviceScopes / mqttWriteDeviceScopes are marshalled from the
	// Scopes registry, which InitScopes populates.
	utils.InitScopes()
}

// TestSetIdentityRoundTrips checks that an identity and its scopes survive the
// store-and-read cycle, and that a device identity carries no scopes.
func TestSetIdentityRoundTrips(t *testing.T) {
	cl := &mochi.Client{}
	setIdentity(cl, kindUser, "prn:pantahub.com:auth:/alice", scopeAll)

	kind, subject := identity(cl)
	if kind != kindUser || subject != "prn:pantahub.com:auth:/alice" {
		t.Fatalf("identity = (%q,%q), want user/alice", kind, subject)
	}
	if got := clientScopes(cl); len(got) != 1 || got[0] != scopeAll {
		t.Fatalf("clientScopes = %v, want [%q]", got, scopeAll)
	}

	dev := &mochi.Client{}
	setIdentity(dev, kindDevice, "5f0000000000000000000001", "")
	if got := clientScopes(dev); len(got) != 0 {
		t.Fatalf("device clientScopes = %v, want empty", got)
	}
}

// TestSetIdentityReplacesClientSuppliedProps ensures a client cannot forge an
// identity or scopes by presetting the user-property keys the ACL hook reads.
func TestSetIdentityReplacesClientSuppliedProps(t *testing.T) {
	cl := &mochi.Client{}
	cl.Properties.Props.User = []packets.UserProperty{
		{Key: propKind, Val: kindDevice},
		{Key: propSubject, Val: "attacker"},
		{Key: propScopes, Val: scopeAll},
	}

	// The authenticated identity is a scopeless user; the forged device kind,
	// subject and scopes must all be gone.
	setIdentity(cl, kindUser, "prn:pantahub.com:auth:/alice", "")

	kind, subject := identity(cl)
	if kind != kindUser || subject != "prn:pantahub.com:auth:/alice" {
		t.Fatalf("identity = (%q,%q), want the authenticated user", kind, subject)
	}
	if got := clientScopes(cl); len(got) != 0 {
		t.Fatalf("clientScopes = %v, want none (forged scope dropped)", got)
	}
}

// TestUserWithoutScopeIsDeniedDeviceAccess is the regression for the scope
// bypass: a user token narrowed to an unrelated scope must be refused both
// reads and command writes before any ownership lookup, exactly as REST would
// refuse it. The denial is reached without a mongo client, proving it short
// circuits ahead of userOwns.
func TestUserWithoutScopeIsDeniedDeviceAccess(t *testing.T) {
	h := &authHook{} // no mongo client: userOwns would panic/deny if reached
	cl := &mochi.Client{}
	// A scope that grants nothing on devices.
	setIdentity(cl, kindUser, "prn:pantahub.com:auth:/alice", "prn:pantahub.com:apis:/base/metrics")

	deviceID := "5f0000000000000000000001"
	readTopic := Topic(deviceID, SuffixUserMeta)
	writeTopic := Topic(deviceID, SuffixCommands)

	if h.OnACLCheck(cl, readTopic, false) {
		t.Fatal("read allowed for a token without a device read scope")
	}
	if h.OnACLCheck(cl, writeTopic, true) {
		t.Fatal("command write allowed to a user")
	}
}

// TestUsersNeverPublish: commands go through the REST API only, so no user
// token, whatever its scopes, may publish on the message plane.
func TestUsersNeverPublish(t *testing.T) {
	h := &authHook{} // no mongo client: denial must come before userOwns
	for _, scope := range []string{scopeAll, scopeReadOnly} {
		cl := &mochi.Client{}
		setIdentity(cl, kindUser, "prn:pantahub.com:auth:/alice", scope)
		for _, suffix := range []string{SuffixCommands, SuffixCommandsResult, SuffixDeviceMeta, SuffixStatus} {
			if h.OnACLCheck(cl, Topic("5f0000000000000000000001", suffix), true) {
				t.Fatalf("user with %s allowed to publish %s", scope, suffix)
			}
		}
	}
}

// TestScopeSetsCoverReads guards the read scope list: the API ("all") and
// read-only scopes both read.
func TestScopeSetsCoverReads(t *testing.T) {
	if !utils.MatchScope(mqttReadDeviceScopes, []string{scopeAll}) ||
		!utils.MatchScope(mqttReadDeviceScopes, []string{scopeReadOnly}) {
		t.Fatal("the API and read-only scopes must satisfy device reads")
	}
}

// TestDeviceCannotClaimAnotherDevicesSession pins the client-id binding: the
// session is keyed by client id and a second CONNECT with the same id evicts
// the first, so a device presenting another device's id — valid credentials
// and all — would be a cross-device eviction loop. A device may only claim
// the session named after itself; user identities keep free ids.
func TestDeviceCannotClaimAnotherDevicesSession(t *testing.T) {
	dev := &mochi.Client{}
	setIdentity(dev, kindDevice, "5f0000000000000000000001", "")

	if !mayClaimSession(dev, "5f0000000000000000000001") {
		t.Fatal("a device was denied its own session")
	}
	if mayClaimSession(dev, "5f0000000000000000000002") {
		t.Fatal("a device claimed another device's session")
	}
	if mayClaimSession(dev, "") {
		t.Fatal("a device claimed the empty client id")
	}

	user := &mochi.Client{}
	setIdentity(user, kindUser, "prn:pantahub.com:auth:/alice", scopeAll)
	if !mayClaimSession(user, "dashboard-alice-1") {
		t.Fatal("a user identity was denied a free client id")
	}
}

// TestCommandResultsAreDeviceWrittenOnly pins the command topic split: a user
// never publishes (commands go through the REST API) and so never forges a
// result, and a device may answer but never issue a command to itself.
// Neither case reaches an ownership lookup.
func TestCommandResultsAreDeviceWrittenOnly(t *testing.T) {
	h := &authHook{} // no mongo client: userOwns would panic/deny if reached
	deviceID := "5f0000000000000000000001"

	user := &mochi.Client{}
	setIdentity(user, kindUser, "prn:pantahub.com:auth:/alice", scopeAll)
	if h.OnACLCheck(user, Topic(deviceID, SuffixCommandsResult), true) {
		t.Fatal("a user was allowed to publish a command result")
	}

	dev := &mochi.Client{}
	setIdentity(dev, kindDevice, deviceID, "")
	if !h.OnACLCheck(dev, Topic(deviceID, SuffixCommandsResult), true) {
		t.Fatal("a device was denied publishing its own command result")
	}
	if h.OnACLCheck(dev, Topic(deviceID, SuffixCommands), true) {
		t.Fatal("a device was allowed to publish a command")
	}
	if !h.OnACLCheck(dev, Topic(deviceID, SuffixCommands), false) {
		t.Fatal("a device was denied subscribing to its commands")
	}
	if h.OnACLCheck(dev, Topic("5f0000000000000000000002", SuffixCommandsResult), true) {
		t.Fatal("a device was allowed to answer for another device")
	}
}

// A client id already in use by another identity is refused: taking the
// session over would hand the newcomer its subscriptions and queued messages.
func TestSessionOfAnotherIdentityIsNotTakenOver(t *testing.T) {
	server := mochi.New(&mochi.Options{InlineClient: true, Logger: discardLogger})
	h := &authHook{server: server}

	alice := server.NewClient(nil, "test", "dashboard", false)
	setIdentity(alice, kindUser, "prn:pantahub.com:auth:/alice", scopeAll)
	server.Clients.Add(alice)

	bob := server.NewClient(nil, "test", "dashboard", false)
	setIdentity(bob, kindUser, "prn:pantahub.com:auth:/bob", scopeAll)
	if h.mayTakeOverSession(bob) {
		t.Fatal("bob took over alice's session")
	}

	aliceAgain := server.NewClient(nil, "test", "dashboard", false)
	setIdentity(aliceAgain, kindUser, "prn:pantahub.com:auth:/alice", scopeAll)
	if !h.mayTakeOverSession(aliceAgain) {
		t.Fatal("alice was refused her own session")
	}

	fresh := server.NewClient(nil, "test", "dashboard-2", false)
	setIdentity(fresh, kindUser, "prn:pantahub.com:auth:/bob", scopeAll)
	if !h.mayTakeOverSession(fresh) {
		t.Fatal("a free client id was refused")
	}

	// A device's session is only ever the device's.
	device := server.NewClient(nil, "test", "5f0000000000000000000001", false)
	setIdentity(device, kindDevice, "5f0000000000000000000001", "")
	server.Clients.Add(device)
	user := server.NewClient(nil, "test", "5f0000000000000000000001", false)
	setIdentity(user, kindUser, "prn:pantahub.com:auth:/alice", scopeAll)
	if h.mayTakeOverSession(user) {
		t.Fatal("a user took over a device's session")
	}
}

// User sessions end with their connection, whatever the client asked for;
// device sessions stay as requested.
func TestUserSessionsEndOnDisconnect(t *testing.T) {
	user := &mochi.Client{}
	user.Properties.Clean = false
	user.Properties.Props.SessionExpiryInterval = 3600
	user.Properties.Props.SessionExpiryIntervalFlag = true
	setIdentity(user, kindUser, "prn:pantahub.com:auth:/alice", scopeAll)
	userSessionsEndOnDisconnect(user)
	if !user.Properties.Clean || user.Properties.Props.SessionExpiryInterval != 0 {
		t.Fatalf("user session kept: clean %v, expiry %d", user.Properties.Clean, user.Properties.Props.SessionExpiryInterval)
	}

	device := &mochi.Client{}
	device.Properties.Props.SessionExpiryInterval = 3600
	setIdentity(device, kindDevice, "5f0000000000000000000001", "")
	userSessionsEndOnDisconnect(device)
	if device.Properties.Clean || device.Properties.Props.SessionExpiryInterval != 3600 {
		t.Fatal("device session changed")
	}
}

// Ownership answers expire, so a device that changes owner stops reaching
// the previous owner's open connection.
func TestOwnershipCacheExpires(t *testing.T) {
	cl := &mochi.Client{}
	setIdentity(cl, kindUser, "prn:pantahub.com:auth:/alice", scopeAll)
	deviceID := "5f0000000000000000000001"
	now := time.Now()

	cacheOwnershipAt(cl, deviceID, true, now)
	if owns, cached := cachedOwnershipAt(cl, deviceID, now.Add(ownershipCacheTTL-time.Second)); !owns || !cached {
		t.Fatal("fresh answer not cached")
	}
	if _, cached := cachedOwnershipAt(cl, deviceID, now.Add(ownershipCacheTTL)); cached {
		t.Fatal("stale answer reused")
	}

	// Refreshing replaces the entry rather than adding one.
	cacheOwnershipAt(cl, deviceID, false, now.Add(ownershipCacheTTL))
	if owns, cached := cachedOwnershipAt(cl, deviceID, now.Add(ownershipCacheTTL)); owns || !cached {
		t.Fatal("refreshed answer not used")
	}
	entries := 0
	for _, prop := range cl.Properties.Props.User {
		if strings.HasPrefix(prop.Key, propOwnsPrefix) {
			entries++
		}
	}
	if entries != 1 {
		t.Fatalf("%d cache entries for one device", entries)
	}
}
