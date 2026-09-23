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

// Package logs provides the abstract logging infrastructure for pantahub
// logging endpoint as well as backends for elastic and mgo.
//
// Logs offers a simple logging service for Pantahub powered devices and apps.
// To post new log entries use the POST method on the main endpoint
// To page through log entries and sort etc. check the GET method
package logs

import (
	"encoding/json"
	"net/http"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
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
func (a *App) handleGetLogsCursor(c *echo.Context) error {

	var err error

	authType, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["type"]

	if authType != "USER" && authType != "SESSION" {
		return echoutil.RestErrorWrapper(c, "Need to be logged in as USER/SESSION user to get logs", http.StatusForbidden)
	}

	own, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		return echoutil.RestErrorWrapper(c, "You need to be logged in", http.StatusForbidden)
	}

	// This route is served for GET as well as POST, and a GET carries no body,
	// so a body that is missing or unparseable is not an error here -- the
	// cursor is then expected in the query string below.
	var nextCursorJWT string
	jsonBody := map[string]interface{}{}
	if err = echoutil.DecodeJsonPayload(c, &jsonBody); err == nil {
		if nextCursor, ok := jsonBody["next-cursor"].(string); ok {
			nextCursorJWT = nextCursor
		}
	}
	// if body doesnt have the cursor lets try query
	if nextCursorJWT == "" {
		_ = c.Request().ParseForm()
		nextCursorJWT = c.Request().FormValue("next-cursor")
	}

	// A missing cursor is a malformed request, not an authentication problem.
	// Answering 403 here made clients treat it as an expired session and send
	// the user back to a login prompt.
	if nextCursorJWT == "" {
		return echoutil.RestErrorWrapper(c, "no next-cursor supplied", http.StatusBadRequest)
	}

	token, err := jwtgo.ParseWithClaims(nextCursorJWT, &CursorClaim{}, func(token *jwtgo.Token) (interface{}, error) {
		return a.jwtConfig.Pub, nil
	})

	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error decoding JWT token for next-cursor: "+err.Error(), http.StatusForbidden)
	}

	if claims, ok := token.Claims.(*CursorClaim); ok && token.Valid {
		var result *Pager

		// v5 Audience is a slice; require exactly one, equal to the caller (not membership).
		caller := claims.RegisteredClaims.Audience
		if len(caller) != 1 || caller[0] != own {
			return echoutil.RestErrorWrapper(c, "Calling user does not match owner of cursor-next", http.StatusForbidden)
		}

		state := claims.State
		if state == nil {
			return echoutil.RestErrorWrapper(c, "next-cursor carries no query state", http.StatusBadRequest)
		}

		// The owner is re-asserted from the caller's own token rather than
		// trusted from the cursor, so a cursor can never widen what its bearer
		// is allowed to read.
		filter := state.Filter
		filter.Owner = own.(string)

		result, err = a.backend.getLogs(c.Request().Context(), 0, state.Page, state.Before, state.After,
			&filter, state.Sort, state.SearchAfter, true)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "ERROR: getting logs failed "+err.Error(), http.StatusInternalServerError)
		}

		// Always hand a cursor back, so a follower polling an idle device keeps
		// something valid to present. When the page was empty the position is
		// unchanged, so the previous search_after is carried forward and the
		// next call returns whatever arrived in the meantime.
		nextState := &CursorState{
			Filter:      filter,
			Before:      state.Before,
			After:       state.After,
			Sort:        state.Sort,
			Page:        state.Page,
			SearchAfter: state.SearchAfter,
		}
		if result.NextCursor != "" {
			if err := json.Unmarshal([]byte(result.NextCursor), &nextState.SearchAfter); err != nil {
				return echoutil.RestErrorWrapper(c, "ERROR: building next-cursor: "+err.Error(), http.StatusInternalServerError)
			}
		}
		ss, err := a.signCursor(nextState, own.(string))
		if err != nil {
			return echoutil.RestErrorWrapper(c, "ERROR: signing next-cursor token: "+err.Error(), http.StatusInternalServerError)
		}
		result.NextCursor = ss

		return echoutil.WriteJSON(c, http.StatusOK, result)
	}

	return echoutil.RestErrorWrapper(c, "Unexpected Code", http.StatusInternalServerError)
}
