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
	"context"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/tracer"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"gopkg.in/mgo.v2/bson"
)

// ## POST /logs/
//
// handleGetLogs Post one or many log entries as an error of LogEntry
// @Summary Post one or many log entries as an error of LogEntry
// @Description Post one or many log entries as an error of LogEntry
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags logs
// @Param body body Entry true "New logs body"
// @Success 200 {array} Entry
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /logs [post]
func (a *App) handlePostLogs(w rest.ResponseWriter, r *rest.Request) {
	ctx := context.WithoutCancel(r.Context())
	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]
	if !ok {
		utils.RestErrorWrapper(w, "can't read token type", http.StatusForbidden)
		return
	}
	if authType != "DEVICE" {
		utils.RestErrorWrapper(w, "Need to be logged in as DEVICE to post logs", http.StatusForbidden)
		return
	}

	device, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["owner"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in as device with owner", http.StatusForbidden)
		return
	}

	entries, err := readLogsBody(ctx, r.Body)
	if err != nil {
		utils.RestErrorWrapper(w, err.Error(), http.StatusBadRequest)
		return
	}

	newEntries := []Entry{}

	for _, v := range entries {
		v.ID, err = primitive.ObjectIDFromHex(bson.NewObjectId().Hex())
		if err != nil {
			utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
			return
		}
		v.Device = device.(string)
		v.Owner = owner.(string)
		v.TimeCreated = time.Now()
		if v.LogLevel == "" {
			v.LogLevel = "INFO"
		}
		newEntries = append(newEntries, v)
	}

	err = a.backend.postLogs(ctx, newEntries)
	if err != nil {
		utils.RestErrorWrapper(w, "Error posting logs "+err.Error(), http.StatusInternalServerError)
		log.Println("ERROR: Error posting logs " + err.Error())
		return
	}

	w.WriteJson(newEntries)
}

func readLogsBody(ctx context.Context, buff io.ReadCloser) ([]Entry, error) {
	t := tracer.GetFunctionTracer()
	if t != nil {
		span := t.NewSpan(ctx, "readLogsBody")
		defer span.End()
	}

	body, err := ioutil.ReadAll(buff)
	if err != nil {
		return nil, errors.New("error reading logs body")
	}

	entries, err := unmarshalBody(body)
	if err != nil {
		return nil, fmt.Errorf("error parsing logs body: %s '%s'", err, body)
	}

	return entries, nil
}
