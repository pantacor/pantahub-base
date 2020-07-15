//
// Copyright 2018  Pantacor Ltd.
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

package utils

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/mgo.v2/bson"
)

// AuthMiddleware authentication default middleware
type AuthMiddleware struct{}

// AuthInfo authentication information
type AuthInfo struct {
	Caller     Prn
	CallerType string
	Owner      Prn
	Roles      string
	Audience   string
	Scopes     []string
	Nick       string
	RemoteUser string
}

// GetAuthInfo get authentication information from a request
func GetAuthInfo(r *rest.Request) *AuthInfo {
	authInfo, ok := r.Env["PH_AUTH_INFO"]
	if !ok {
		return nil
	}
	rs := authInfo.(AuthInfo)
	return &rs
}

// MiddlewareFunc authentication middleware function
func (s *AuthMiddleware) MiddlewareFunc(handler rest.HandlerFunc) rest.HandlerFunc {
	return func(w rest.ResponseWriter, r *rest.Request) {
		var origCallerClaims, callerClaims jwtgo.MapClaims
		env := r.Env

		origCallerClaims = r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)
		callerClaims = origCallerClaims

		if callerClaims["call-as"] != nil {
			callerClaims = jwtgo.MapClaims(callerClaims["call-as"].(map[string]interface{}))
			callerClaims["exp"] = origCallerClaims["exp"]
			callerClaims["orig_iat"] = origCallerClaims["orig_iat"]
		}
		r.Env["JWT_PAYLOAD"] = callerClaims
		r.Env["JWT_ORIG_PAYLOAD"] = origCallerClaims

		authInfo := AuthInfo{}
		caller, ok := callerClaims["prn"]
		if !ok {
			// XXX: find right error
			RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
			return
		}
		callerStr := caller.(string)
		prn := Prn(callerStr)
		authInfo.Caller = prn

		authType, ok := callerClaims["type"]
		if !ok {
			// XXX: find right error
			RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
			return
		}
		authTypeStr := authType.(string)
		authInfo.CallerType = authTypeStr

		owner, ok := callerClaims["owner"]
		if ok {
			ownerStr := owner.(string)
			prn := Prn(ownerStr)
			authInfo.Owner = prn
		}
		roles, ok := callerClaims["roles"]
		if ok {
			rolesStr := roles.(string)
			authInfo.Roles = rolesStr
		}
		aud, ok := callerClaims["aud"]
		if ok {
			audStr := aud.(string)
			authInfo.Audience = audStr
		}
		scopes, ok := callerClaims["scopes"]
		if ok {
			scopesStr := scopes.(string)
			authInfo.Scopes = strings.Fields(scopesStr)
		}
		nick, ok := callerClaims["nick"]
		if ok {
			nickStr := nick.(string)
			authInfo.Nick = nickStr
		}
		origNick, ok := origCallerClaims["nick"]
		if ok {
			origNickStr := origNick.(string)
			authInfo.RemoteUser = origNickStr + "==>" + authInfo.Nick
		} else {
			authInfo.RemoteUser = "_unknown_==>" + authInfo.Nick
		}

		env["PH_AUTH_INFO"] = authInfo

		r.Env = env
		handler(w, r)
	}
}

// ValidateOwnerSig valdiate a owner signature
func ValidateOwnerSig(sig, tokenID, owner, name string, col *mongo.Collection) error {
	signature, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		return errors.New("decode signature: " + err.Error())
	}

	token, err := getToken(tokenID, owner, col)
	if err != nil {
		return errors.New("Token not found or you are not the owner: " + err.Error())
	}

	// Validate signature
	tokenSha := token.TokenSha[32:] // remove the first 32 bytes padding on the token sha in database
	nameBytes := []byte(name)
	idevidNameHex := make([]byte, hex.EncodedLen(len(nameBytes)))
	_ = hex.Encode(idevidNameHex, nameBytes)

	validSignature := validMAC(idevidNameHex, signature, tokenSha)
	if !validSignature {
		return errors.New("Invalid ownership signature signature")
	}

	return nil
}

// ValidMAC reports whether messageMAC is a valid HMAC tag for message.
func validMAC(message, messageMAC, key []byte) bool {
	mac := hmac.New(sha256.New, key)
	mac.Write(message)
	expectedMAC := mac.Sum(nil)

	return hmac.Equal(messageMAC, expectedMAC)
}

func getToken(tokenID, owner string, col *mongo.Collection) (*PantahubDevicesJoinToken, error) {
	res := &PantahubDevicesJoinToken{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cleanToken := strings.TrimSuffix(tokenID, "\n")
	id, err := primitive.ObjectIDFromHex(cleanToken)
	if err != nil {
		return nil, err
	}

	err = col.FindOne(ctx, bson.M{
		"_id":      id,
		"owner":    owner,
		"disabled": bson.M{"$ne": true},
	}).Decode(res)
	if err != nil {
		return nil, err
	}

	return res, nil
}

// ParseBase64PemCert parse der certificate to x590 certificate
func ParseBase64PemCert(certPemBase64 string) (*x509.Certificate, error) {
	certPEM, err := base64.StdEncoding.DecodeString(certPemBase64)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode([]byte(certPEM))
	if block == nil {
		return nil, errors.New("failed to parse certificate PEM")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}

	return cert, nil
}

// ValidateCaSigned validate a certificate that has been signed by pantahub CA
func ValidateCaSigned(cert *x509.Certificate) error {
	caCertPem, err := base64.StdEncoding.DecodeString(GetEnv(EnvPantahubCaCert))
	if err != nil {
		return err
	}

	block, _ := pem.Decode([]byte(caCertPem))
	if block == nil {
		return errors.New("failed to parse certificate PEM")
	}

	caCert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return err
	}

	rootPool := x509.NewCertPool()
	rootPool.AddCert(caCert)

	_, err = cert.Verify(x509.VerifyOptions{
		Roots: rootPool,
	})

	return err
}
