package mqtt

import (
	"strconv"
	"testing"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"gitlab.com/pantacor/pantahub-base/devices"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// The broker and its packet/session handling are real. Only the Mongo device
// lookup is replaced with a fixed owned device; the production auth hook still
// handles ACL checks, expiry, clean-session policy and disconnects.
type ownedDeviceAuthHook struct {
	*authHook
	device devices.Device
}

func (h *ownedDeviceAuthHook) ID() string { return "owned-device-auth-test" }

func (h *ownedDeviceAuthHook) OnConnectAuthenticate(cl *mochi.Client, pk packets.Packet) bool {
	claims, ok := parseToken(string(pk.Connect.Password))
	if !ok {
		return false
	}
	if !authenticateWithExpiringToken(cl, &h.device, claims) {
		return false
	}
	if !userSessionStartsClean(cl, pk) {
		return false
	}
	if !mayClaimSession(cl, pk.Connect.ClientIdentifier) || !h.mayTakeOverSession(cl) {
		return false
	}
	userSessionsEndOnDisconnect(cl)
	cacheOwnership(cl, h.device.ID.Hex(), true)
	return true
}

func authTestBroker(t *testing.T) (*mochi.Server, string) {
	t.Helper()
	testJWTKey(t)
	server := mochi.New(&mochi.Options{InlineClient: true, Logger: discardLogger})
	id := primitive.NewObjectID()
	hook := &ownedDeviceAuthHook{
		authHook: &authHook{server: server},
		device:   devices.Device{ID: id, Owner: testAlice},
	}
	if err := server.AddHook(hook, nil); err != nil {
		t.Fatal(err)
	}
	if err := server.Serve(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = server.Close() })
	return server, id.Hex()
}

func userConnectParams(deviceID, clientID, token string, clean bool) packets.ConnectParams {
	return packets.ConnectParams{
		Clean: clean, Keepalive: 60, ClientIdentifier: clientID,
		Username: []byte(devicePrnPrefix + deviceID), Password: []byte(token),
	}
}

func TestJWTExpiryIsPrivateAndRequired(t *testing.T) {
	id := primitive.NewObjectID()
	device := &devices.Device{ID: id, Owner: testAlice}
	cl := &mochi.Client{}
	cl.Properties.Props.User = []packets.UserProperty{{Key: propExpiresAt, Val: "0"}}
	setIdentity(cl, kindUser, testAlice, scopeReadOnly)
	if _, ok := tokenExpiry(cl); ok {
		t.Fatal("client-supplied expiry survived authentication")
	}
	if authenticateWithExpiringToken(cl, device, jwtgo.MapClaims{
		"type": "USER", "prn": testAlice, "scopes": scopeReadOnly,
	}) {
		t.Fatal("a JWT without an expiry opened an MQTT connection")
	}
	expiresAt := time.Now().Add(-time.Second).Truncate(time.Second)
	if !authenticateWithExpiringToken(cl, device, jwtgo.MapClaims{
		"type": "USER", "prn": testAlice, "scopes": scopeReadOnly, "exp": float64(expiresAt.Unix()),
	}) {
		t.Fatal("expiring token was not recorded")
	}
	if (&authHook{}).OnACLCheck(cl, Topic(id.Hex(), SuffixLogs), false) {
		t.Fatal("expired token still reads an MQTT topic")
	}
}

func TestJWTConnectionClosesAtExpiry(t *testing.T) {
	server, deviceID := authTestBroker(t)
	// signToken defaults to an hour; this one has an intentionally short lease.
	expiresAt := time.Now().Add(3 * time.Second).Truncate(time.Second)
	token, err := jwtgo.NewWithClaims(jwtgo.SigningMethodRS256, jwtgo.MapClaims{
		"type": "USER", "prn": testAlice, "scopes": scopeReadOnly, "exp": expiresAt.Unix(),
	}).SignedString(testJWTKey(t))
	if err != nil {
		t.Fatal(err)
	}
	clients := make([]*wireClient, 0, 2)
	for _, version := range []byte{4, 5} {
		client, code := dialWire(t, server, version,
			userConnectParams(deviceID, "expiry-test-"+strconv.Itoa(int(version)), token, true))
		if code != packets.CodeSuccess.Code {
			t.Fatalf("v%d CONNECT refused: %#x", version, code)
		}
		if codes := client.subscribe(t, Topic(deviceID, SuffixLogs)); len(codes) != 1 || !granted(codes[0]) {
			t.Fatalf("v%d log subscription refused before expiry: %v", version, codes)
		}
		clients = append(clients, client)
	}
	for _, client := range clients {
		client.expectClosed(t)
	}
	if _, ok := parseToken(token); ok {
		t.Fatal("the token was still valid when the broker closed the connection")
	}
}

func TestUserReconnectCannotReplayBroaderScopes(t *testing.T) {
	server, deviceID := authTestBroker(t)
	clientID := "scope-replay-test"
	wide := signToken(t, jwtgo.MapClaims{"type": "USER", "prn": testAlice, "scopes": scopeReadOnly})
	original, code := dialWire(t, server, 5, userConnectParams(deviceID, clientID, wide, true))
	if code != packets.CodeSuccess.Code {
		t.Fatalf("wide CONNECT refused: %#x", code)
	}
	topic := Topic(deviceID, SuffixLogs)
	if codes := original.subscribe(t, topic); len(codes) != 1 || !granted(codes[0]) {
		t.Fatalf("log subscription refused: %v", codes)
	}
	if err := server.Publish(topic, []byte("private log"), false, 1); err != nil {
		t.Fatal(err)
	}
	if pk := original.nextPublish(t, time.Second); string(pk.Payload) != "private log" {
		t.Fatalf("initial delivery: %q", pk.Payload)
	}
	// Leave that QoS 1 delivery unacknowledged. A resumed broker session would
	// replay it before checking the new token's narrower scopes.
	narrow := signToken(t, jwtgo.MapClaims{"type": "USER", "prn": testAlice, "scopes": "prn:pantahub.com:apis:/base/metrics"})
	_, code = dialWire(t, server, 5, userConnectParams(deviceID, clientID, narrow, false))
	if code == packets.CodeSuccess.Code {
		t.Fatal("user CONNECT with session inheritance was accepted")
	}

	clean, code := dialWire(t, server, 5, userConnectParams(deviceID, clientID, narrow, true))
	if code != packets.CodeSuccess.Code {
		t.Fatalf("clean reconnect refused: %#x", code)
	}
	clean.expectSilence(t, 150*time.Millisecond)
}
