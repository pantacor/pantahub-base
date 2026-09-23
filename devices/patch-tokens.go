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

package devices

import (
	"errors"
	"fmt"
	"net/http"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// patchDeviceTokenRequest defines the fields that can be updated for a device token.
type patchDeviceTokenRequest struct {
	Nick            string                  `json:"nick,omitempty"`
	OVMode          *models.OVModeExtension `json:"ovmode,omitempty"`
	DefaultUserMeta map[string]string       `json:"default-user-meta,omitempty"`
}

// handlePatchTokens Update a device token (Nick and OVMode only)
// @Summary Update a device token (Nick and OVMode only)
// @Description Update specific fields (Nick, OVMode) of a device token.
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID of the token to update"
// @Param tokenBody body patchDeviceTokenRequest true "Fields to update"
// @Success 200 {object} utils.PantahubDevicesJoinToken
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/tokens/{id} [patch]
func (a *App) handlePatchTokens(c *echo.Context) error {

	jwtPayload, ok := echoutil.Lookup(c, echoutil.KeyJWTPayload)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD", http.StatusBadRequest)
	}

	var caller interface{}
	caller, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD item 'prn'", http.StatusBadRequest)
	}

	var authType interface{}
	authType, ok = jwtPayload.(jwtgo.MapClaims)["type"]
	if !ok {
		return echoutil.RestErrorWrapper(c, "Missing JWT_PAYLOAD item 'type'", http.StatusBadRequest)
	}

	if authType != "USER" && authType != "SESSION" {
		return echoutil.RestErrorWrapper(c, "Can not be updated by Device", http.StatusBadRequest)
	}

	tokenID := c.Param("id")
	tokenIDBson, err := primitive.ObjectIDFromHex(tokenID)
	if err != nil {
		message := fmt.Sprintf("Invalid token ID format: %s", err.Error())
		return echoutil.RestErrorWrapper(c, message, http.StatusBadRequest)
	}

	// Parse request body
	patchReq := patchDeviceTokenRequest{}
	err = echoutil.DecodeJsonPayload(c, &patchReq)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error parsing request body: "+err.Error(), http.StatusBadRequest)
	}

	patch := JoinTokenPatch{OVMode: patchReq.OVMode}
	if patchReq.Nick != "" {
		patch.Nick = &patchReq.Nick
	}
	if patchReq.DefaultUserMeta != nil {
		patch.DefaultUserMeta = map[string]interface{}{}
		for key, val := range patchReq.DefaultUserMeta {
			patch.DefaultUserMeta[key] = val
		}
	}
	if patch.Nick == nil && patch.OVMode == nil && patch.DefaultUserMeta == nil {
		return echoutil.RestErrorWrapper(c, "No updatable fields provided in request body (nick or ovmode)", http.StatusBadRequest)
	}

	updatedToken, err := PatchJoinToken(c.Request().Context(), a.mongoClient, caller.(string), tokenIDBson, patch)
	if errors.Is(err, ErrJoinTokenNotFound) {
		return echoutil.RestErrorWrapper(c, "Device token not found or not owned by caller", http.StatusNotFound)
	}
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error updating device token: "+err.Error(), http.StatusInternalServerError)
	}

	return echoutil.WriteJSON(c, http.StatusOK, updatedToken)
}
