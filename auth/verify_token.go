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

// Package auth authetication package
package auth

import (
	"net/http"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

// requestPayload payload to verify token
type requestPayload struct {
	Service    string `json:"service"`
	TokenID    string `json:"token-id"`
	Owner      string `json:"owner"`
	IDevIDName string `json:"idevid-name"`
	Signature  string `json:"signature"`
}

// verifyToken Verify device token from TPM device validation
// @Summary Verify device token from TPM device validation
// @Description Verify device token from TPM device validation
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param body body requestPayload true "Token payload"
// @Success 200
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /auth/signature/verify [post]
func (a *App) verifyToken(c *echo.Context) error {
	payload := &requestPayload{}
	if err := echoutil.DecodeJsonPayload(c, payload); err != nil {
		return echoutil.RestErrorWrapper(c, "Error decoding json payload: "+err.Error(), http.StatusBadRequest)
	}

	col := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	err := utils.ValidateOwnerSig(c.Request().Context(), payload.Signature, payload.TokenID, payload.Owner, payload.IDevIDName, col)
	if err != nil {
		return echoutil.Error(c, err.Error(), http.StatusBadRequest)
	}

	response := []byte("ok")
	_ = echoutil.WriteHeader(c, http.StatusOK)
	_, _ = c.Response().Write(response)
	return nil
}
