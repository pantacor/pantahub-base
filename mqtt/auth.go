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
	"context"
	"crypto/rsa"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// devicePrnPrefix is the PRN namespace of a device account. The MQTT username
// is always a device PRN: a device presents its own, a user presents the PRN of
// a device it reaches through this connection.
const devicePrnPrefix = "prn:::devices:/"

// Identity keys stashed in the client's MQTT v5 user properties. The whole
// property slice is replaced on successful CONNECT, so a client cannot forge an
// identity by sending user properties with these keys in its own CONNECT.
const (
	propKind    = "ph-identity-kind"
	propSubject = "ph-identity-subject"

	// propScopes carries the space-separated OAuth scopes of a user or session
	// token, so the ACL hook can hold a scope-narrowed token to the same
	// device privileges the REST plane would.
	propScopes = "ph-identity-scopes"

	// propOwnsPrefix + <device-id> caches the per-connection outcome of an
	// ownership lookup, "1" for owned and "0" for definitely not owned.
	propOwnsPrefix = "ph-owns/"
)

// Device topic access for a user or session token mirrors the scope filters the
// REST device endpoints enforce (devices.Service): reading device state needs a
// read-capable scope. A token minted through /tokens with a narrowed scope set
// is thereby held to the same privileges on the message plane as on REST. Users
// never publish (see OnACLCheck).
var (
	mqttReadDeviceScopes = utils.MarshalScopes([]utils.Scope{
		utils.Scopes.API,
		utils.Scopes.APIReadOnly,
		utils.Scopes.Devices,
		utils.Scopes.ReadDevices,
	})
)

// Identity kinds. A device may only reach its own namespace; a user may reach
// the namespaces of the devices it owns.
const (
	kindDevice = "device"
	kindUser   = "user"
)

const (
	// connectTimeout bounds the Mongo work done once per connection.
	connectTimeout = 10 * time.Second

	// aclTimeout bounds the ownership lookup done at most once per device per
	// connection. It is shorter than connectTimeout because it sits in the
	// message path, where a stalled database must fail fast rather than pin a
	// publisher's goroutine.
	aclTimeout = 5 * time.Second

	// maxOwnershipCacheEntries caps the per-connection ownership cache so a
	// client cannot grow its own client struct without bound by touching an
	// endless stream of device ids. Beyond the cap lookups still work, they
	// just stop being cached.
	maxOwnershipCacheEntries = 64

	// ownershipCacheTTL bounds how long an ownership answer is reused. The
	// ACL is checked again on every message delivered to a subscriber, so a
	// device that changes owner stops reaching the previous owner's open
	// connection within this time.
	ownershipCacheTTL = time.Minute
)

var (
	errNoMongoClient = errors.New("mqtt: no mongo client configured")

	// errInvalidDeviceID marks a device id that can never name a device, as
	// opposed to a lookup that failed for infrastructure reasons.
	errInvalidDeviceID = errors.New("mqtt: malformed device id")
)

// jwtPublicKey loads the RSA public key the REST API verifies its tokens with,
// once per process. utils.GetJwtRsaKeys reads PANTAHUB_JWT_SECRET and
// PANTAHUB_JWT_PUB, the same pair base.DoInit hands to the JWT middleware, so a
// token accepted here is exactly a token accepted by the REST API.
var jwtPublicKey = sync.OnceValues(func() (*rsa.PublicKey, error) {
	keys, err := utils.GetJwtRsaKeys("", "")
	if err != nil {
		return nil, err
	}
	return keys.PublicKey, nil
})

// authHook authenticates MQTT clients against the Pantahub device and account
// model and authorizes every publish and subscribe against the topic namespace
// defined in topics.go.
type authHook struct {
	mochi.HookBase
	mongoClient *mongo.Client

	// server is the broker the hook authenticates for, to look up the
	// session a CONNECT would take over. Nil in tests that do not need it.
	server *mochi.Server
}

// ID identifies the hook to the broker.
func (h *authHook) ID() string {
	return "pantahub-auth"
}

// Provides advertises only the two events this hook implements.
func (h *authHook) Provides(b byte) bool {
	switch b {
	case mochi.OnConnectAuthenticate, mochi.OnACLCheck:
		return true
	}
	return false
}

// OnConnectAuthenticate authenticates a CONNECT packet.
//
// The username is always a device PRN. The password is either the device secret
// — checked by authservices.DeviceAuth, the same check POST /auth/login runs —
// or a JWT issued by the REST API, which may be the device's own token or the
// token of a user or session.
//
// A connection is refused when the device does not exist, is garbage collected,
// or has no owner: unclaimed devices must finish claiming over REST first.
// It is also refused while owner verification is a TLS challenge that has not
// completed, because the REST tokens of such a device are deliberately narrowed
// to ownership validation (see authservices.DevicePayload) and that restriction
// must hold on the message plane too.
func (h *authHook) OnConnectAuthenticate(cl *mochi.Client, pk packets.Packet) bool {
	if h.mongoClient == nil {
		return false
	}

	username := string(pk.Connect.Username)
	password := string(pk.Connect.Password)
	if username == "" || password == "" {
		return false
	}
	if !strings.HasPrefix(username, devicePrnPrefix) {
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), connectTimeout)
	defer cancel()

	device, err := h.lookupDevice(ctx, utils.PrnGetID(username))
	if err != nil {
		return false
	}
	if device.Owner == "" {
		return false
	}
	if device.OVMode.NeedsVerification() {
		return false
	}

	if claims, ok := parseToken(password); ok {
		if !authenticateWithToken(cl, device, claims) {
			return false
		}
	} else {
		// Not a token: fall back to the device secret. DeviceAuth re-reads the
		// device document; a connection is rare enough that the second read is
		// cheaper than duplicating the credential comparison here.
		if !authservices.DeviceAuth(username, password, h.mongoClient) {
			return false
		}
		setIdentity(cl, kindDevice, device.ID.Hex(), "")
	}

	// The broker keys sessions by client id and hands a session to whoever
	// presents its id: a second CONNECT with the same id evicts the first
	// (session takeover). The ACL already stops a colliding id from touching
	// another device's topics, but not from evicting that device over and
	// over — a cross-device denial of service that needs nothing more than
	// valid credentials for *any* device. Bind the id at CONNECT instead: a
	// device identity may only claim the session named after itself.
	if !mayClaimSession(cl, pk.Connect.ClientIdentifier) {
		return false
	}

	if !h.mayTakeOverSession(cl) {
		return false
	}
	userSessionsEndOnDisconnect(cl)

	// A Last Will is published and retained by the broker with no ACL check of
	// its own (mochi's sendLWT bypasses OnACLCheck), so an accepted will lets a
	// client write any topic the moment it disconnects ungracefully. Gate it
	// here, at CONNECT, with the very same ACL a live publish faces: a will the
	// authenticated identity could not publish is rejected, taking the whole
	// connection with it, rather than being armed for later delivery.
	if pk.Connect.WillFlag && !h.OnACLCheck(cl, pk.Connect.WillTopic, true) {
		return false
	}

	return true
}

// mayClaimSession reports whether the authenticated identity may open an MQTT
// session under this client id. A device is bound to its own device id — the
// id pvsm and every other device client already uses — so no device can take
// over another device's session. User and session identities keep free ids
// for their ephemeral dashboards, except ids shaped like a device id: the
// broker's session takeover disconnects the occupant, so a user claiming a
// device-shaped id could evict that device's push channel over and over.
func mayClaimSession(cl *mochi.Client, clientID string) bool {
	kind, subject := identity(cl)
	if kind == kindDevice {
		return clientID == subject
	}
	_, err := primitive.ObjectIDFromHex(clientID)
	return err != nil
}

// mayTakeOverSession reports whether cl may take over the session the broker
// holds under its client id, if any. A session held by someone else is never
// taken over: the broker hands the newcomer the session's subscriptions and
// replays its queued messages without any ACL check, so the id of another
// user's session would be a way into that user's device traffic (and a way to
// evict that user over and over).
func (h *authHook) mayTakeOverSession(cl *mochi.Client) bool {
	if h.server == nil {
		return true
	}
	existing, ok := h.server.Clients.Get(cl.ID)
	return !ok || sameIdentity(existing, cl)
}

// sameIdentity reports whether two clients authenticated as the same subject.
func sameIdentity(a, b *mochi.Client) bool {
	kindA, subjectA := identity(a)
	kindB, subjectB := identity(b)
	return kindA != "" && kindA == kindB && subjectA == subjectB
}

// userSessionsEndOnDisconnect makes the session of a user identity end with
// its connection, whatever the client asked for. Users watch devices live and
// have no use for queued delivery, and a persistent session would hold up to
// the broker's inflight maximum of that user's device traffic for the whole
// session expiry, for whoever presents its client id next.
func userSessionsEndOnDisconnect(cl *mochi.Client) {
	if kind, _ := identity(cl); kind != kindUser {
		return
	}
	cl.Lock()
	defer cl.Unlock()
	cl.Properties.Clean = true
	cl.Properties.Props.SessionExpiryInterval = 0
	cl.Properties.Props.SessionExpiryIntervalFlag = false
}

// OnACLCheck authorizes a single publish (write) or subscribe (read). It never
// touches Mongo for a device identity, and at most once per device and
// ownershipCacheTTL for a user identity, because the resolved identity and the
// ownership answers are kept on the client.
//
// Topics outside the versioned device namespace are denied, and so are wildcard
// filters: Parse leaves the wildcard in the device id or the suffix, neither of
// which can match a scope or a permitted suffix.
func (h *authHook) OnACLCheck(cl *mochi.Client, topic string, write bool) bool {
	deviceID, suffix, ok := Parse(topic)
	if !ok {
		return false
	}

	kind, subject := identity(cl)
	switch kind {
	case kindDevice:
		if !strings.HasPrefix(topic, DeviceScope(subject)) {
			return false
		}
		if write {
			return DeviceMayPublish(suffix)
		}
		return DeviceMaySubscribe(suffix)

	case kindUser:
		// A user reads anything a device it owns exposes, within the read
		// scopes the token carries, and never publishes. Commands reach a
		// device only through POST /devices/{id}/commands, which enforces the
		// allowlist, the command scope, the rate limits and the audit record;
		// a direct publish would skip all of them.
		if write {
			return false
		}
		if !utils.MatchScope(mqttReadDeviceScopes, clientScopes(cl)) {
			return false
		}
		return h.userOwns(cl, subject, deviceID)
	}

	return false
}

// authenticateWithToken accepts a REST-issued token for the device named in the
// username. Device tokens must belong to that device; user and session tokens
// carry their own PRN and are authorized per topic by ownership. Any other
// token type — service, client, third-party app — is refused: those identities
// have no device scope of their own to derive from.
func authenticateWithToken(cl *mochi.Client, device *devices.Device, claims jwtgo.MapClaims) bool {
	claims = effectiveClaims(claims)

	callerPrn, _ := claims["prn"].(string)
	if callerPrn == "" {
		return false
	}

	callerType, _ := claims["type"].(string)
	switch accounts.AccountType(callerType) {
	case accounts.AccountTypeDevice:
		if !strings.HasPrefix(callerPrn, devicePrnPrefix) || utils.PrnGetID(callerPrn) != device.ID.Hex() {
			return false
		}
		setIdentity(cl, kindDevice, device.ID.Hex(), "")
		return true

	case accounts.AccountTypeUser, accounts.AccountTypeSessionUser:
		// The scopes travel with the identity so the ACL hook can enforce them
		// per topic without re-parsing the token. An absent claim is no scopes,
		// which the REST plane also treats as insufficient for device access.
		callerScopes, _ := claims["scopes"].(string)
		setIdentity(cl, kindUser, callerPrn, callerScopes)
		return true
	}

	return false
}

// parseToken verifies a bearer token against the REST API's public key and
// signing algorithm, exactly as the JWT middleware's parseToken does for an
// Authorization header. Expiry is enforced by the claim validation jwtgo runs
// as part of Parse.
func parseToken(raw string) (jwtgo.MapClaims, bool) {
	pub, err := jwtPublicKey()
	if err != nil {
		return nil, false
	}

	token, err := jwtgo.Parse(raw, func(t *jwtgo.Token) (interface{}, error) {
		if jwtgo.GetSigningMethod("RS256") != t.Method {
			return nil, errors.New("invalid signing algorithm")
		}
		return pub, nil
	})
	if err != nil || token == nil || !token.Valid {
		return nil, false
	}

	claims, ok := token.Claims.(jwtgo.MapClaims)
	if !ok {
		return nil, false
	}

	// A token bound to an OAuth protected resource is only good at that
	// resource, exactly as on the REST API (echoutil.JWT).
	if utils.IsResourceBoundAudience(claims["aud"]) {
		return nil, false
	}

	return claims, true
}

// effectiveClaims resolves admin impersonation the same way
// utils.AuthMiddleware does, so a token that acts as another account on the
// REST API acts as that same account here.
func effectiveClaims(claims jwtgo.MapClaims) jwtgo.MapClaims {
	callAs, ok := claims["call-as"].(map[string]interface{})
	if !ok {
		return claims
	}
	return jwtgo.MapClaims(callAs)
}

// userOwns reports whether callerPrn owns deviceID, answering from the
// per-connection cache when possible.
func (h *authHook) userOwns(cl *mochi.Client, callerPrn, deviceID string) bool {
	if owns, cached := cachedOwnership(cl, deviceID); cached {
		return owns
	}

	ctx, cancel := context.WithTimeout(context.Background(), aclTimeout)
	defer cancel()

	device, err := h.lookupDevice(ctx, deviceID)
	if err != nil {
		// Only a definitive answer is cached. A timeout or a dropped
		// connection to Mongo denies this message but must not pin the
		// denial for the rest of the connection.
		if errors.Is(err, mongo.ErrNoDocuments) || errors.Is(err, errInvalidDeviceID) {
			cacheOwnership(cl, deviceID, false)
		}
		return false
	}

	owns := device.Owner != "" && device.Owner == callerPrn
	cacheOwnership(cl, deviceID, owns)
	return owns
}

// lookupDevice reads a non-garbage device by its hex id. The filter matches
// authservices.DeviceAuth so that a device invisible to the REST login is
// invisible here as well.
func (h *authHook) lookupDevice(ctx context.Context, id string) (*devices.Device, error) {
	if h.mongoClient == nil {
		return nil, errNoMongoClient
	}

	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, errInvalidDeviceID
	}

	device := &devices.Device{}
	err = h.mongoClient.Database(utils.MongoDb).
		Collection(devicesCollection).
		FindOne(ctx, bson.M{
			"_id":     objectID,
			"garbage": bson.M{"$ne": true},
		}).Decode(device)
	if err != nil {
		return nil, err
	}

	return device, nil
}

// The identity and the ownership answers live in the client's MQTT v5 user
// properties. mochi copies them per connection in Client.ParseConnect and never
// reads or transmits them itself, so they are private per-connection storage
// that is released with the client — unlike a hook-side map, which would need a
// disconnect event this hook deliberately does not subscribe to. Access is
// serialized with the client's own mutex because OnACLCheck runs concurrently:
// a subscriber's ACL check is performed on the publisher's goroutine.

// setIdentity records the authenticated identity, replacing every user property
// the client sent so that none of the keys read below can be attacker supplied.
// scopes is stored only when non-empty, keeping a device identity's properties
// exactly as they were.
func setIdentity(cl *mochi.Client, kind, subject, scopes string) {
	cl.Lock()
	defer cl.Unlock()

	props := []packets.UserProperty{
		{Key: propKind, Val: kind},
		{Key: propSubject, Val: subject},
	}
	if scopes != "" {
		props = append(props, packets.UserProperty{Key: propScopes, Val: scopes})
	}
	cl.Properties.Props.User = props
}

// identity returns the kind and subject recorded at CONNECT. An unauthenticated
// client has neither, which denies every topic.
func identity(cl *mochi.Client) (kind, subject string) {
	cl.RLock()
	defer cl.RUnlock()

	for _, prop := range cl.Properties.Props.User {
		switch prop.Key {
		case propKind:
			kind = prop.Val
		case propSubject:
			subject = prop.Val
		}
	}

	if kind == "" || subject == "" {
		return "", ""
	}

	return kind, subject
}

// clientScopes returns the scopes recorded for a user identity at CONNECT, as
// the REST plane's space-separated fields. A device identity, or a user token
// that carried no scopes claim, yields none — which denies every scoped check.
func clientScopes(cl *mochi.Client) []string {
	cl.RLock()
	defer cl.RUnlock()

	for _, prop := range cl.Properties.Props.User {
		if prop.Key == propScopes {
			return strings.Fields(prop.Val)
		}
	}

	return nil
}

// cachedOwnership returns an ownership answer for deviceID resolved less than
// ownershipCacheTTL ago. Entries are "<1|0>:<unix seconds resolved>".
func cachedOwnership(cl *mochi.Client, deviceID string) (owns, cached bool) {
	return cachedOwnershipAt(cl, deviceID, time.Now())
}

func cachedOwnershipAt(cl *mochi.Client, deviceID string, now time.Time) (owns, cached bool) {
	cl.RLock()
	defer cl.RUnlock()

	key := propOwnsPrefix + deviceID
	for _, prop := range cl.Properties.Props.User {
		if prop.Key != key {
			continue
		}
		answer, at, ok := strings.Cut(prop.Val, ":")
		if !ok {
			return false, false
		}
		resolved, err := strconv.ParseInt(at, 10, 64)
		if err != nil || now.Sub(time.Unix(resolved, 0)) >= ownershipCacheTTL {
			return false, false
		}
		return answer == "1", true
	}

	return false, false
}

// cacheOwnership records an ownership answer for ownershipCacheTTL.
func cacheOwnership(cl *mochi.Client, deviceID string, owns bool) {
	cacheOwnershipAt(cl, deviceID, owns, time.Now())
}

func cacheOwnershipAt(cl *mochi.Client, deviceID string, owns bool, now time.Time) {
	cl.Lock()
	defer cl.Unlock()

	value := "0:"
	if owns {
		value = "1:"
	}
	value += strconv.FormatInt(now.Unix(), 10)

	key := propOwnsPrefix + deviceID
	entries := 0
	for i, prop := range cl.Properties.Props.User {
		if prop.Key == key {
			cl.Properties.Props.User[i].Val = value
			return
		}
		if strings.HasPrefix(prop.Key, propOwnsPrefix) {
			entries++
		}
	}

	if entries >= maxOwnershipCacheEntries {
		return
	}

	cl.Properties.Props.User = append(cl.Properties.Props.User, packets.UserProperty{Key: key, Val: value})
}
