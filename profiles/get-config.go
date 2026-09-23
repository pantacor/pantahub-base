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

package profiles

import (
	"net/http"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/bson"
)

// handleGetGlobalMeta Get user profile global meta
// @Summary Get user profile global meta
// @Description Get user profile global meta
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags user
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /profiles/metas [get]
func (a *App) handleGetGlobalMeta(c *echo.Context) error {
	jwtPayload, ok := echoutil.Lookup(c, echoutil.KeyJWTPayload)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Token owner can't be defined", http.StatusInternalServerError)
	}
	tokenOwner, ok := jwtPayload.(jwtgo.MapClaims)["prn"].(string)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Token owner can't be defined", http.StatusInternalServerError)
	}

	account, err := a.getUserAccount(c.Request().Context(), tokenOwner, "prn")
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Account "+err.Error(), http.StatusInternalServerError)
	}

	haveProfile, err := a.ExistsInProfiles(c.Request().Context(), account.ID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), http.StatusForbidden)
	}

	if !haveProfile {
		_, err := a.MakeUserProfile(c.Request().Context(), account, nil)
		if err != nil {
			return echoutil.RestErrorWrapper(c, err.Error(), http.StatusForbidden)
		}
	}

	profile, err := a.getProfile(c.Request().Context(), account.Prn, bson.M{"meta": 1})
	if err != nil {
		return echoutil.RestErrorWrapper(c, "No Access", http.StatusForbidden)
	}

	if account.Prn != tokenOwner {
		return echoutil.RestErrorWrapperUser(c, "not the profile owner", "Profile is not public", http.StatusForbidden)
	}

	return echoutil.WriteJSON(c, http.StatusOK, utils.BsonUnquoteMap(&profile.Meta))
}
