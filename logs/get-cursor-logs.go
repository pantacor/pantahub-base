// Copyright 2020  Pantacor Ltd.
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

// Package logs provides the abstract logging infrastructure for pantahub
// logging endpoint as well as backends for elastic and mgo.
//
// Logs offers a simple logging service for Pantahub powered devices and apps.
// To post new log entries use the POST method on the main endpoint
// To page through log entries and sort etc. check the GET method
package logs

import (
	"net/http"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// handleGetLogsCursor Get or postlog cursor
// @Summary Get or one or many log entries
// @Description Get or one or many log entries
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags logs
// @Param next-cursor formData string false "next-cursor ID"
// @Success 200 {object} Pager
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /logs/cursor [get]
func (a *App) handleGetLogsCursor(w rest.ResponseWriter, r *rest.Request) {

	var err error

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if authType != "USER" && authType != "SESSION" {
		utils.RestErrorWrapper(w, "Need to be logged in as USER/SESSION user to get logs", http.StatusForbidden)
		return
	}

	own, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	jsonBody := map[string]interface{}{}
	err = r.DecodeJsonPayload(&jsonBody)
	if err != nil {
		utils.RestErrorWrapper(w, "Error decoding json request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	var nextCursorJWT string
	nextCursor := jsonBody["next-cursor"]
	if nextCursor == nil {
		nextCursorJWT = ""
	} else {
		nextCursorJWT = nextCursor.(string)
	}
	// if body doesnt have the cursor lets try query
	if nextCursorJWT == "" {
		r.ParseForm()
		nextCursorJWT = r.FormValue("next-cursor")
	}

	token, err := jwtgo.ParseWithClaims(nextCursorJWT, &CursorClaim{}, func(token *jwtgo.Token) (interface{}, error) {
		return a.jwtMiddleware.Pub, nil
	})

	if err != nil {
		utils.RestErrorWrapper(w, "Error decoding JWT token for next-cursor: "+err.Error(), http.StatusForbidden)
		return
	}

	if claims, ok := token.Claims.(*CursorClaim); ok && token.Valid {
		var result *Pager

		caller := claims.StandardClaims.Audience
		if caller != own {
			utils.RestErrorWrapper(w, "Calling user does not match owner of cursor-next", http.StatusForbidden)
			return
		}
		nextCursor := claims.NextCursor
		result, err = a.backend.getLogsByCursor(r.Context(), nextCursor)
		if err != nil {
			utils.RestErrorWrapper(w, "ERROR: getting logs failed "+err.Error(), http.StatusInternalServerError)
			return
		}

		if result.NextCursor != "" {
			claims := CursorClaim{
				NextCursor: result.NextCursor,
				StandardClaims: jwtgo.StandardClaims{
					ExpiresAt: time.Now().Add(time.Duration(time.Minute * 2)).Unix(),
					IssuedAt:  time.Now().Unix(),
					Audience:  own.(string),
				},
			}
			token := jwtgo.NewWithClaims(jwtgo.GetSigningMethod(a.jwtMiddleware.SigningAlgorithm), claims)
			ss, err := token.SignedString(a.jwtMiddleware.Key)
			if err != nil {
				utils.RestErrorWrapper(w, "ERROR: signing scrollid token: "+err.Error(), http.StatusInternalServerError)
				return
			}
			result.NextCursor = ss
		}

		w.WriteJson(result)
		return
	}

	utils.RestErrorWrapper(w, "Unexpected Code", http.StatusInternalServerError)
	return
}
