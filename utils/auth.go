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
	"net/http"
	"strings"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
)

type AuthMiddleware struct {
}

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

func GetAuthInfo(r *rest.Request) *AuthInfo {
	authInfo, ok := r.Env["PH_AUTH_INFO"]
	if !ok {
		return nil
	}
	rs := authInfo.(AuthInfo)
	return &rs
}

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
			rest.Error(w, "You need to be logged in", http.StatusForbidden)
			return
		}
		callerStr := caller.(string)
		prn := Prn(callerStr)
		authInfo.Caller = prn

		authType, ok := callerClaims["type"]
		if !ok {
			// XXX: find right error
			rest.Error(w, "You need to be logged in", http.StatusForbidden)
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

//ScopeFilter :  Scope Filter for end points
func ScopeFilter(filterScopes []string, handler rest.HandlerFunc) rest.HandlerFunc {

	filterScopes = parseScopes(filterScopes)

	return func(w rest.ResponseWriter, r *rest.Request) {
		authInfo := GetAuthInfo(r)
		if authInfo != nil && len(filterScopes) > 0 {
			if !matchScope(filterScopes, authInfo.Scopes) {
				phAuth := GetEnv(ENV_PANTAHUB_AUTH)
				w.Header().Set("WWW-Authenticate", `Bearer Realm="pantahub services",
								ph-aeps="`+phAuth+`",
								scope="`+strings.Join(filterScopes, " ")+`",
								error="insufficient_scope",
								error_description="The request requires higher privileges than provided by the
				     access token"
								`)
				rest.Error(w, "InSufficient Scopes", http.StatusForbidden)
				return
			}
		}
		handler(w, r)
	}
}

func matchScope(filterScopes []string, requestScopes []string) bool {

	for _, fs := range filterScopes {
		for _, rs := range requestScopes {
			if fs == rs {
				return true
			}
		}
	}
	return false
}
func parseScopes(scopes []string) []string {
	for k, scope := range scopes {
		scopes[k] = "prn:pantahub.com:apis:/base/" + scope
	}
	return scopes
}
