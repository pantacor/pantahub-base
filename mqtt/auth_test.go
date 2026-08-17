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

package mqtt

import (
	"testing"

	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// userScope is the space-separated scope string a login/API token carries.
const (
	scopeAll      = "prn:pantahub.com:apis:/base/all"
	scopeReadOnly = "prn:pantahub.com:apis:/base/all.readonly"
)

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
		t.Fatal("command write allowed for a token without a device write scope")
	}
}

// TestReadOnlyScopeCannotWriteCommands checks the read/write split: a read-only
// token is denied command writes even though it may read.
func TestReadOnlyScopeCannotWriteCommands(t *testing.T) {
	h := &authHook{}
	cl := &mochi.Client{}
	setIdentity(cl, kindUser, "prn:pantahub.com:auth:/alice", scopeReadOnly)

	if h.OnACLCheck(cl, Topic("5f0000000000000000000001", SuffixCommands), true) {
		t.Fatal("read-only token was allowed to write commands")
	}
}

// TestScopeSetsCoverReadAndWrite guards the mirrored scope lists against drift:
// the API ("all") scope must satisfy both, the read-only scope only reads, and
// the write scope only writes.
func TestScopeSetsCoverReadAndWrite(t *testing.T) {
	if !utils.MatchScope(mqttReadDeviceScopes, []string{scopeAll}) ||
		!utils.MatchScope(mqttWriteDeviceScopes, []string{scopeAll}) {
		t.Fatal("the API scope must satisfy both read and write device access")
	}
	if !utils.MatchScope(mqttReadDeviceScopes, []string{scopeReadOnly}) {
		t.Fatal("the read-only scope must satisfy device reads")
	}
	if utils.MatchScope(mqttWriteDeviceScopes, []string{scopeReadOnly}) {
		t.Fatal("the read-only scope must not satisfy device writes")
	}
}
