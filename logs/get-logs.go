// Copyright 2017  Pantacor Ltd.
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
	"strconv"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// ## GET /logs/
//   Post one or many log entries as an error of LogEntry
//   Page through your logs.
//
//   Context:
//      Can be called in user context
//
//   Paging Parameter:
//     - start: list position to start page; either number or ID or
//	            "<tsec>.<tnano>" of log entry
//     - page: length of page
//
//   Filter Paramters:
//     - dev: comma separated list of device prns  to include
//     - lvl: comma separated list of log levels
//     - src: comma separated list of sources
//
//   Sorting Parameters:
//     - sort: common list of items of "tsec,tnano,device,src,lvl,time-created"
//             you can use - on each individual item to reverse order
//
//   Cursor Parameters:
//     - cursor: true in case you want us to return a cursor ID as well.
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
func (a *App) handleGetLogs(w rest.ResponseWriter, r *rest.Request) {

	var result *Pager
	var err error

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if authType != "USER" {
		utils.RestErrorWrapper(w, "Need to be logged in as USER to get logs", http.StatusForbidden)
		return
	}

	own, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	r.ParseForm()

	startParam := r.FormValue("start")
	pageParam := r.FormValue("page")

	startParamInt := int64(0)
	if startParam != "" {
		var p int
		p, err = strconv.Atoi(startParam)
		startParamInt = int64(p)
	}
	if err != nil {
		utils.RestErrorWrapper(w, "Bad 'start' parameter", http.StatusBadRequest)
		return
	}

	pageParamInt := int64(50)
	if pageParam != "" {
		var p int
		p, err = strconv.Atoi(pageParam)
		pageParamInt = int64(p)
	}
	if err != nil {
		utils.RestErrorWrapper(w, "Bad 'page' parameter", http.StatusBadRequest)
		return
	}
	revParam := r.FormValue("rev")
	platParam := r.FormValue("plat")
	sourceParam := r.FormValue("src")
	deviceParam := r.FormValue("dev")
	deviceParam, err = a.ParseDeviceString(own.(string), deviceParam)
	if err != nil {
		utils.RestErrorWrapper(w, "Error Parsing Device nicks:"+err.Error(), http.StatusBadRequest)
		return
	}
	levelParam := r.FormValue("lvl")

	filter := &Entry{
		Owner:     own.(string),
		LogLevel:  levelParam,
		LogSource: sourceParam,
		LogRev:    revParam,
		LogPlat:   platParam,
		Device:    deviceParam,
	}

	logsSort := Sorts{}
	sortParam := r.FormValue("sort")

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

	beforeParam := r.FormValue("before")
	afterParam := r.FormValue("after")

	if beforeParam != "" {
		t, err := time.Parse(time.RFC3339, beforeParam)
		if err != nil {
			utils.RestErrorWrapper(w, "ERROR: parsing 'before' date "+err.Error(), http.StatusBadRequest)
			return
		}
		before = &t
	}
	if afterParam != "" {
		t, err := time.Parse(time.RFC3339, afterParam)
		if err != nil {
			utils.RestErrorWrapper(w, "ERROR: parsing 'before' date "+err.Error(), http.StatusBadRequest)
			return
		}
		after = &t
	}

	cursor := r.FormValue("cursor") != ""
	result, err = a.backend.getLogs(startParamInt, pageParamInt, before, after, filter, logsSort, cursor)

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
}
