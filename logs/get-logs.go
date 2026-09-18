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
	"strconv"
	"strings"
	"time"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

var maxPagination = int64(500)

// cursorTTL is how long a next-cursor token stays usable. The cursor is
// stateless (it just replays the query with a search_after bound), so this is
// only a replay window and no longer has to stay under any server-side scroll
// keep-alive.
const cursorTTL = 15 * time.Minute

// ## GET /logs/
//
//	  Post one or many log entries as an error of LogEntry
//	  Page through your logs.
//
//	  Context:
//	     Can be called in user context
//
//	  Paging Parameter:
//	    - start: list position to start page; either number or ID or
//		            "<tsec>.<tnano>" of log entry
//	    - page: length of page
//
//	  Filter Paramters:
//	    - dev: comma separated list of device prns  to include
//	    - lvl: comma separated list of log levels
//	    - src: comma separated list of sources
//
//	  Sorting Parameters:
//	    - sort: common list of items of "tsec,tnano,device,src,lvl,time-created"
//	            you can use - on each individual item to reverse order
//
//	  Cursor Parameters:
//	    - cursor: true in case you want us to return a cursor ID as well.
//
// handleGetLogs Get one or many log entries as an error of LogEntry
// @Summary Get one or many log entries as an error of LogEntry
// @Description Get one or many log entries as an error of LogEntry
// @Description Page through your logs.
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags logs
// @Param start query string false "list position to start page; either number or ID or '<tsec>.<tnano>' of log entry"
// @Param page query string false "length of page"
// @Param dev query string false "comma separated list of device prns  to include"
// @Param lvl query string false "comma separated list of log levels"
// @Param src query string false "comma separated list of log levels"
// @Param sort query string false "common list of items of 'tsec,tnano,device,src,lvl,time-created' you can use - on each individual item to reverse order"
// @Param cursor query string false "true in case you want us to return a cursor ID as well."
// @Success 200 {object} Pager
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /logs [get]
func (a *App) handleGetLogs(c *echo.Context) error {

	var result *Pager
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

	_ = c.Request().ParseForm()

	startParam := c.Request().FormValue("start")
	pageParam := c.Request().FormValue("page")

	startParamInt := int64(0)
	if startParam != "" {
		var p int
		p, err = strconv.Atoi(startParam)
		startParamInt = int64(p)
	}
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Bad 'start' parameter", http.StatusBadRequest)
	}

	pageParamInt := int64(50)
	if pageParam != "" {
		var p int
		p, err = strconv.Atoi(pageParam)
		pageParamInt = int64(p)
	}

	if pageParamInt > maxPagination {
		pageParamInt = maxPagination
	}
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Bad 'page' parameter", http.StatusBadRequest)
	}
	revParam := c.Request().FormValue("rev")
	platParam := c.Request().FormValue("plat")
	sourceParam := c.Request().FormValue("src")
	deviceParam := c.Request().FormValue("dev")
	deviceParam, err = a.ParseDeviceString(c.Request().Context(), own.(string), deviceParam)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error Parsing Device nicks:"+err.Error(), http.StatusBadRequest)
	}
	levelParam := c.Request().FormValue("lvl")

	filter := &Entry{
		Owner:     own.(string),
		LogLevel:  levelParam,
		LogSource: sourceParam,
		LogRev:    revParam,
		LogPlat:   platParam,
		Device:    deviceParam,
	}

	logsSort := Sorts{}
	sortParam := c.Request().FormValue("sort")

	sorts := strings.Split(sortParam, ",")
	for _, v := range sorts {
		switch v1 := strings.TrimPrefix(v, "-"); v1 {
		case "dev":
			fallthrough
		case "rev":
			fallthrough
		case "plat":
			fallthrough
		case "lvl":
			fallthrough
		case "tsec":
			fallthrough
		case "tnano":
			fallthrough
		case "time-created":
			fallthrough
		case "src":
			logsSort = append(logsSort, v)
		}
	}

	var before *time.Time
	var after *time.Time

	beforeParam := c.Request().FormValue("before")
	afterParam := c.Request().FormValue("after")

	if beforeParam != "" {
		t, err := time.Parse(time.RFC3339, beforeParam)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "ERROR: parsing 'before' date "+err.Error(), http.StatusBadRequest)
		}
		before = &t
	}
	if afterParam != "" {
		t, err := time.Parse(time.RFC3339, afterParam)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "ERROR: parsing 'before' date "+err.Error(), http.StatusBadRequest)
		}
		after = &t
	}

	cursor := c.Request().FormValue("cursor") != ""
	result, err = a.backend.getLogs(c.Request().Context(), startParamInt, pageParamInt, before, after, filter, logsSort, nil, cursor)

	if err != nil {
		return echoutil.RestErrorWrapper(c, "ERROR: getting logs failed "+err.Error(), http.StatusInternalServerError)
	}

	// A caller that asked for a cursor always gets one back, even when this
	// page came up empty. Followers such as `pvr device logs` call the cursor
	// endpoint with whatever they were last handed, so dropping the cursor on
	// an empty page left them posting an empty one, which cannot be a valid
	// token and came back as a 403 that clients read as "log in again". An
	// empty page simply carries the position forward: re-presenting the cursor
	// returns whatever has arrived since.
	if cursor {
		state := &CursorState{
			Filter: *filter,
			Before: before,
			After:  after,
			Sort:   logsSort,
			Page:   pageParamInt,
		}
		if result.NextCursor != "" {
			if err := json.Unmarshal([]byte(result.NextCursor), &state.SearchAfter); err != nil {
				return echoutil.RestErrorWrapper(c, "ERROR: building next-cursor: "+err.Error(), http.StatusInternalServerError)
			}
		}
		ss, err := a.signCursor(state, own.(string))
		if err != nil {
			return echoutil.RestErrorWrapper(c, "ERROR: signing next-cursor token: "+err.Error(), http.StatusInternalServerError)
		}
		result.NextCursor = ss
	}

	return echoutil.WriteJSON(c, http.StatusOK, result)
}

// signCursor wraps the state needed to fetch the next page in a short-lived
// token addressed to the calling user, so that a cursor cannot be replayed by
// anyone else.
func (a *App) signCursor(state *CursorState, owner string) (string, error) {
	claims := CursorClaim{
		State: state,
		RegisteredClaims: jwtgo.RegisteredClaims{
			ExpiresAt: jwtgo.NewNumericDate(time.Now().Add(cursorTTL)),
			IssuedAt:  jwtgo.NewNumericDate(time.Now()),
			Audience:  jwtgo.ClaimStrings{owner},
		},
	}
	token := jwtgo.NewWithClaims(jwtgo.GetSigningMethod(a.jwtConfig.SigningAlgorithm), claims)
	return token.SignedString(a.jwtConfig.Key)
}
