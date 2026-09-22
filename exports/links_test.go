// Copyright (c) 2026 Pantacor Ltd.
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

package exports

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"net/url"
	"strings"
	"testing"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/pantacor/pantahub-base/utils"
)

func useTestKeys(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	encode := func(kind string, der []byte) string {
		return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: kind, Bytes: der}))
	}
	t.Setenv(utils.EnvPantahubJWTAuthSecret, encode("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key)))
	t.Setenv(utils.EnvPantahubJWTAuthPub, encode("PUBLIC KEY", pub))
	linkKeys = loadLinkKeys()
	return key
}

func tokenOf(t *testing.T, link string) string {
	t.Helper()
	parsed, err := url.Parse(link)
	require.NoError(t, err)
	segments := strings.Split(strings.TrimPrefix(parsed.Path, "/"), "/")
	require.GreaterOrEqual(t, len(segments), 3, parsed.Path)
	return segments[len(segments)-2]
}

func TestDownloadLinkNamesOneExportAndNothingElse(t *testing.T) {
	key := useTestKeys(t)

	link, expires, err := SignDownloadLink(LinkRequest{
		OwnerPrn: "prn:pantahub.com:auth:/user1", OwnerNick: "user1",
		DeviceNick: "alpha_one", Rev: 7, Parts: []string{"bsp", "web"},
	}, 5*time.Minute)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(5*time.Minute), expires, 5*time.Second)
	assert.True(t, strings.HasSuffix(link, "/alpha_one-7.tar.gz"), link)

	claims, err := parseLinkToken(tokenOf(t, link))
	require.NoError(t, err)
	assert.Equal(t, "prn:pantahub.com:auth:/user1", claims["sub"])
	assert.Equal(t, "alpha_one", claims[claimLinkDevice])
	assert.EqualValues(t, 7, claims[claimLinkRev])
	assert.Equal(t, "bsp,web", claims[claimLinkParts])

	// Bound to a URL, so the REST API, MQTT and the MCP endpoint refuse it as
	// a login.
	assert.True(t, utils.IsResourceBoundAudience(claims["aud"]))

	// Expired, forged or mangled links are refused.
	expired := jwtgo.NewWithClaims(jwtgo.SigningMethodRS256, jwtgo.MapClaims{
		"aud": linkAudience(), "sub": "prn:pantahub.com:auth:/user1", "exp": time.Now().Add(-time.Minute).Unix(),
	})
	signed, err := expired.SignedString(key)
	require.NoError(t, err)
	_, err = parseLinkToken(signed)
	assert.Error(t, err)

	other, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	forged, err := jwtgo.NewWithClaims(jwtgo.SigningMethodRS256, jwtgo.MapClaims{
		"aud": linkAudience(), "sub": "prn:pantahub.com:auth:/user1", "exp": time.Now().Add(time.Minute).Unix(),
	}).SignedString(other)
	require.NoError(t, err)
	_, err = parseLinkToken(forged)
	assert.Error(t, err)

	// A login token signed with the same key is not a link.
	login, err := jwtgo.NewWithClaims(jwtgo.SigningMethodRS256, jwtgo.MapClaims{
		"prn": "prn:pantahub.com:auth:/user1", "type": "USER", "exp": time.Now().Add(time.Minute).Unix(),
	}).SignedString(key)
	require.NoError(t, err)
	_, err = parseLinkToken(login)
	assert.Error(t, err)

	_, err = parseLinkToken(tokenOf(t, link) + "x")
	assert.Error(t, err)
}

func TestDownloadLinksAreShortLived(t *testing.T) {
	useTestKeys(t)
	_, expires, err := SignDownloadLink(LinkRequest{
		OwnerPrn: "prn:pantahub.com:auth:/user1", OwnerNick: "user1", DeviceNick: "d", Rev: 0,
	}, 24*time.Hour)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(MaxLinkTTL), expires, 5*time.Second)

	_, _, err = SignDownloadLink(LinkRequest{OwnerPrn: "p", OwnerNick: "n", Rev: 0}, time.Minute)
	assert.Error(t, err, "a link names a device")
}
