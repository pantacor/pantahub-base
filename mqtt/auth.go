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
	"context"
	"crypto/rsa"
	"errors"
	"strings"
	"sync"
	"time"

	jwtgo "github.com/dgrijalva/jwt-go"
	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/models"
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

	// propOwnsPrefix + <device-id> caches the per-connection outcome of an
	// ownership lookup, "1" for owned and "0" for definitely not owned.
	propOwnsPrefix = "ph-owns/"
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
	if device.OVMode != nil && device.OVMode.Mode == models.TLSVerification && device.OVMode.Status != models.Completed {
		return false
	}

	if claims, ok := parseToken(password); ok {
		return authenticateWithToken(cl, device, claims)
	}

	// Not a token: fall back to the device secret. DeviceAuth re-reads the
	// device document; a connection is rare enough that the second read is
	// cheaper than duplicating the credential comparison here.
	if !authservices.DeviceAuth(username, password, h.mongoClient) {
		return false
	}

	setIdentity(cl, kindDevice, device.ID.Hex())
	return true
}

// OnACLCheck authorizes a single publish (write) or subscribe (read). It never
// touches Mongo for a device identity, and at most once per device for a user
// identity, because the resolved identity and the ownership answers are kept on
// the client for the life of the connection.
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
		// A user reads anything a device it owns exposes, but only ever
		// writes instructions to it.
		if write && suffix != SuffixCommands {
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
		setIdentity(cl, kindDevice, device.ID.Hex())
		return true

	case accounts.AccountTypeUser, accounts.AccountTypeSessionUser:
		setIdentity(cl, kindUser, callerPrn)
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
func setIdentity(cl *mochi.Client, kind, subject string) {
	cl.Lock()
	defer cl.Unlock()

	cl.Properties.Props.User = []packets.UserProperty{
		{Key: propKind, Val: kind},
		{Key: propSubject, Val: subject},
	}
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

// cachedOwnership returns a previously resolved ownership answer for deviceID.
func cachedOwnership(cl *mochi.Client, deviceID string) (owns, cached bool) {
	cl.RLock()
	defer cl.RUnlock()

	key := propOwnsPrefix + deviceID
	for _, prop := range cl.Properties.Props.User {
		if prop.Key == key {
			return prop.Val == "1", true
		}
	}

	return false, false
}

// cacheOwnership records an ownership answer for the life of this connection.
func cacheOwnership(cl *mochi.Client, deviceID string, owns bool) {
	cl.Lock()
	defer cl.Unlock()

	key := propOwnsPrefix + deviceID
	entries := 0
	for _, prop := range cl.Properties.Props.User {
		if prop.Key == key {
			return
		}
		if strings.HasPrefix(prop.Key, propOwnsPrefix) {
			entries++
		}
	}

	if entries >= maxOwnershipCacheEntries {
		return
	}

	value := "0"
	if owns {
		value = "1"
	}
	cl.Properties.Props.User = append(cl.Properties.Props.User, packets.UserProperty{Key: key, Val: value})
}
