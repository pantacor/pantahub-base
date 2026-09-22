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
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"testing"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// A token a user granted to an MCP client is signed with the API's key like any
// other. It must not open the message plane: its audience says where it is good.
func TestResourceBoundTokenIsNotAnMQTTCredential(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	encode := func(blockType string, der []byte) string {
		return base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: blockType, Bytes: der}))
	}
	// jwtPublicKey reads these once per process; no other test in this package
	// parses a token, so this is the first and only load.
	t.Setenv(utils.EnvPantahubJWTAuthSecret, encode("RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(key)))
	t.Setenv(utils.EnvPantahubJWTAuthPub, encode("PUBLIC KEY", pub))

	sign := func(aud interface{}) string {
		claims := jwtgo.MapClaims{
			"type": "USER", "prn": "prn:pantahub.com:auth:/user1",
			"exp": time.Now().Add(time.Hour).Unix(),
		}
		if aud != nil {
			claims["aud"] = aud
		}
		token, err := jwtgo.NewWithClaims(jwtgo.SigningMethodRS256, claims).SignedString(key)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}

	if _, ok := parseToken(sign(nil)); !ok {
		t.Fatal("an ordinary user token must keep working")
	}
	if _, ok := parseToken(sign("prn:pantahub.com:auth:/service1")); !ok {
		t.Fatal("an on-behalf token, whose audience is a PRN, must keep working")
	}
	if _, ok := parseToken(sign("https://api.example.com/mcp")); ok {
		t.Fatal("a token bound to an mcp endpoint was accepted")
	}
}
