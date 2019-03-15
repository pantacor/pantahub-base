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
	Nick       string
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
		env := r.Env

		authInfo := AuthInfo{}
		caller, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
		if !ok {
			// XXX: find right error
			rest.Error(w, "You need to be logged in", http.StatusForbidden)
			return
		}
		callerStr := caller.(string)
		prn := Prn(callerStr)
		authInfo.Caller = prn

		authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]
		if !ok {
			// XXX: find right error
			rest.Error(w, "You need to be logged in", http.StatusForbidden)
			return
		}
		authTypeStr := authType.(string)
		authInfo.CallerType = authTypeStr

		owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["owner"]
		if ok {
			ownerStr := owner.(string)
			prn := Prn(ownerStr)
			authInfo.Owner = prn
		}
		roles, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["roles"]
		if ok {
			rolesStr := roles.(string)
			authInfo.Roles = rolesStr
		}
		aud, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["aud"]
		if ok {
			audStr := aud.(string)
			authInfo.Audience = audStr
		}
		scopes, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["scopes"]
		if ok {
			scopesStr := scopes.(string)
			authInfo.Audience = scopesStr
		}
		nick, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["nick"]
		if ok {
			nickStr := nick.(string)
			authInfo.Nick = nickStr
		}

		env["PH_AUTH_INFO"] = authInfo
		r.Env = env

		handler(w, r)
	}
}
