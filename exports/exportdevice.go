//
// Copyright (c) 2017-2023 Pantacor Ltd.
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
	"net/http"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/exports/exportservices"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// handleGetExport Export a tar gz file with of a device
// @Summary Export a tar gz file with of a device
// @Description Export a tar gz file with of a device
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags exports
// @Param owner-nick query string false "Owner nick"
// @Param owner query string false "Owner PRN"
// @Success 200 {binary} []byte
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /exports/{owner}/{nick}/{rev}/{filename} [get]
func (a *App) handleGetExport(w rest.ResponseWriter, r *rest.Request) {
	owner := r.PathParam("owner")
	nick := r.PathParam("nick")
	rev := r.PathParam("rev")
	filename := r.PathParam("filename")
	frags := r.URL.Query().Get("parts")

	payload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		utils.RestErrorWrapper(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authIDI, ok := payload.(jwtgo.MapClaims)["prn"]
	if !ok {
		utils.RestErrorWrapper(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authTypeI, ok := payload.(jwtgo.MapClaims)["type"]
	if !ok {
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	ownerPtr, ok := payload.(jwtgo.MapClaims)["owner"]
	if !ok {
		ownerPtr = authIDI
	}

	tokenOwner, ok := ownerPtr.(string)
	if !ok {
		utils.RestErrorWrapper(w, "Session has no owner info", http.StatusBadRequest)
		return
	}

	authType := authTypeI.(string)

	exportservice := exportservices.CreateService(a.mongoClient, utils.MongoDb)

	account, err := exportservice.GetUserAccountByNick(r.Context(), owner)
	if err != nil {
		utils.RestErrorWrapper(w, "Error finding owner user account by nick:"+err.Error(), http.StatusForbidden)
		return
	}

	device, rerr := exportservice.GetDevice(r.Context(), nick, account.Prn, tokenOwner)
	if rerr != nil {
		utils.RestErrorWrite(w, rerr)
		return
	}

	if device.Owner != tokenOwner && !device.IsPublic {
		utils.RestErrorWrapper(w, "Resource not available", http.StatusForbidden)
		return
	}

	revision, state, modtime, rerr := exportservice.GetStepRev(r.Context(), device.ID.Hex(), rev, frags)
	if rerr != nil {
		utils.RestErrorWrite(w, rerr)
		return
	}

	objectDownloads, rerr := exportservice.GetTrailObjects(
		r.Context(),
		device.ID.Hex(),
		revision,
		account.Prn,
		authType,
		device.IsPublic,
		frags,
	)
	if rerr != nil {
		utils.RestErrorWrite(w, rerr)
		return
	}

	exportservice.WriteExportTar(w, filename, objectDownloads, state, modtime)
}
