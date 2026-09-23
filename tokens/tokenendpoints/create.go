// Copyright (c) 2017-2026 Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.
package tokenendpoints

import (
	"errors"
	"net/http"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/tokens/tokenservice"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/mongo"
)

// CreateToken Create a new token for a user
// @Summary Create a new token for a user
// @Description Create a new token for a user
// @Accept json
// @Produce json
// @Tags tokens
// @Security ApiKeyAuth
// @Param Authorization header string true "Insert your access token" default(Bearer <Add access token here>)
// @Param req body tokenservice.AuthTokenReqPayload true "Create Token Request"
// @Success 200 {object} tokenmodels.AuthToken
// @Failure 400 {object} utils.RError
// @Failure 403 {object} utils.RError
// @Failure 409 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /tokens/ [post]
func (app *Endpoints) CreateToken(c *echo.Context) error {
	var owner interface{}

	if jwtPayload, ok := echoutil.Lookup(c, echoutil.KeyJWTPayload); ok {
		if owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"]; !ok {
			return echoutil.RestErrorWrapper(c, "Owner can't be defined", http.StatusBadRequest)
		}
	} else {
		return echoutil.RestErrorWrapper(c, "Owner can't be defined", http.StatusBadRequest)
	}

	payload := tokenservice.AuthTokenReqPayload{}
	if err := echoutil.DecodeJsonPayload(c, &payload); err != nil {
		return echoutil.RestErrorWrapper(c, "Can't process payload", http.StatusBadRequest)
	}

	response, err := app.service.CreateToken(c.Request().Context(), &payload, owner.(string))
	if err != nil {
		if errors.Is(err, tokenservice.ErrInvalidTokenType) {
			return echoutil.RestErrorUser(c, err, "invalid token type", http.StatusBadRequest)
		}

		if mongo.IsDuplicateKeyError(err) {
			return echoutil.RestErrorUser(c, err, "token name is already taken", http.StatusConflict)
		}

		return echoutil.RestErrorUser(c, err, "can't create token", http.StatusInternalServerError)
	}

	if err := echoutil.WriteJSON(c, http.StatusOK, response); err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), http.StatusInternalServerError)
	}
	return nil
}
