//
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

package exports

import (
	"net/http"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/exports/exportservices"
	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
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
func (a *App) handleGetExport(c *echo.Context) error {
	owner := c.Param("owner")
	nick := c.Param("nick")
	rev := c.Param("rev")
	filename := c.Param("filename")
	frags := c.QueryParams().Get("parts")
	meta := c.QueryParams().Get("meta") == "true"

	payload, ok := echoutil.Lookup(c, echoutil.KeyJWTPayload)
	if !ok {
		return echoutil.RestErrorWrapper(c, "You need to be logged in.", http.StatusForbidden)
	}

	authIDI, ok := payload.(jwtgo.MapClaims)["prn"]
	if !ok {
		return echoutil.RestErrorWrapper(c, "You need to be logged in.", http.StatusForbidden)
	}

	authTypeI, ok := payload.(jwtgo.MapClaims)["type"]
	if !ok {
		return echoutil.RestErrorWrapper(c, "You need to be logged in", http.StatusForbidden)
	}

	ownerPtr, ok := payload.(jwtgo.MapClaims)["owner"]
	if !ok {
		ownerPtr = authIDI
	}

	tokenOwner, ok := ownerPtr.(string)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Session has no owner info", http.StatusBadRequest)
	}

	authType := authTypeI.(string)

	exportservice := exportservices.CreateService(a.mongoClient, utils.MongoDb)

	account, err := exportservice.GetUserAccountByNick(c.Request().Context(), owner)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error finding owner user account by nick:"+err.Error(), http.StatusForbidden)
	}

	device, rerr := exportservice.GetDevice(c.Request().Context(), nick, account.Prn, tokenOwner)
	if rerr != nil {
		return echoutil.RestErrorWrite(c, rerr)
	}

	if device.Owner != tokenOwner && !device.IsPublic {
		return echoutil.RestErrorWrapper(c, "Resource not available", http.StatusForbidden)
	}

	revision, state, modtime, rerr := exportservice.GetStepRev(c.Request().Context(), device.ID.Hex(), rev, frags)
	if rerr != nil {
		return echoutil.RestErrorWrite(c, rerr)
	}

	objectDownloads, rerr := exportservice.GetTrailObjects(
		c.Request().Context(),
		device.ID.Hex(),
		revision,
		account.Prn,
		authType,
		device.IsPublic,
		frags,
	)
	if rerr != nil {
		return echoutil.RestErrorWrite(c, rerr)
	}

	// meta=true returns a cheap size estimate (no blob fetch, no tar generation)
	// so clients can show the user the total before starting the download.
	if meta {
		return echoutil.WriteJSON(c, http.StatusOK, buildExportMeta(revision, state, objectDownloads))
	}

	exportservice.WriteExportTar(c, filename, objectDownloads, state, modtime)
	return nil
}

// ExportMeta is the cheap size estimate returned for meta=true requests.
type ExportMeta struct {
	Rev              string `json:"rev"`
	ObjectCount      int    `json:"object_count"`
	UncompressedSize int64  `json:"uncompressed_size"`
}

const tarBlockSize = 512

// tarEntrySize is the on-disk size a tar entry occupies: a 512-byte header plus
// the content padded up to the next 512-byte block boundary.
func tarEntrySize(contentSize int64) int64 {
	padded := ((contentSize + tarBlockSize - 1) / tarBlockSize) * tarBlockSize
	return tarBlockSize + padded
}

// buildExportMeta computes the uncompressed tar size from the state bytes and
// each object's known SizeInt, matching what WriteExportTar would emit: one
// "json" entry, one "objects/<id>" entry per object, and the 1024-byte
// end-of-archive trailer. It is an upper bound for the gzip-compressed download.
func buildExportMeta(rev string, state []byte, objectDownloads []objects.ObjectWithAccess) ExportMeta {
	var total int64 = tarEntrySize(int64(len(state)))
	for _, object := range objectDownloads {
		total += tarEntrySize(object.SizeInt)
	}
	total += 2 * tarBlockSize // two zero blocks terminate the archive

	return ExportMeta{
		Rev:              rev,
		ObjectCount:      len(objectDownloads),
		UncompressedSize: total,
	}
}
