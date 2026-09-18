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
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

// handleGetProfile Get a user profile by user ID
// @Summary Get a user profile by user ID
// Public/Private Profile access logic:
// 1.Check if the user have an active profile or not.
// 2.Check if the user have Public devices or not
// 3.If (1) is FALSE && (2) is TRUE then create a private profile for the user.
// 4.If the user have private profile but have public devices then return only "nick" field as api response.
// 5.If the user have private profile and have no public devices then return error api response.
// 6.If the user have public profile but have no public devices then Mark the profile as private and return error api response
// 7.if the user have public profile then return all the profile details as api response
// @Description Get a user profile by user ID
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags user
// @Param id path string true "ID"
// @Success 200 {array} Profile
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /profiles/{id} [get]
func (a *App) handleGetProfile(c *echo.Context) error {
	accountNick := c.Param("nick")
	var tokenOwner string
	jwtPayload, ok := echoutil.Lookup(c, echoutil.KeyJWTPayload)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Token owner can't be defined", http.StatusInternalServerError)
	}
	tokenOwner, ok = jwtPayload.(jwtgo.MapClaims)["prn"].(string)
	if !ok {
		return echoutil.RestErrorWrapper(c, "Token owner can't be defined", http.StatusInternalServerError)
	}

	account, err := a.getUserAccount(c.Request().Context(), accountNick, "")
	if err != nil {
		switch err.(type) {
		default:
			return echoutil.RestErrorWrapper(c, "Account "+err.Error(), http.StatusInternalServerError)
		}
	}

	haveProfile, err := a.ExistsInProfiles(c.Request().Context(), account.ID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), http.StatusForbidden)
	}

	// Make a new private profile if user have no profile & have public devices
	if !haveProfile {
		_, err := a.MakeUserProfile(c.Request().Context(), account, nil)
		if err != nil {
			return echoutil.RestErrorWrapper(c, err.Error(), http.StatusForbidden)
		}
	}

	profile, err := a.getProfile(c.Request().Context(), account.Prn, nil)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "No Access", http.StatusForbidden)
	}

	if !profile.Public && account.Prn != tokenOwner {
		return echoutil.RestErrorWrapperUser(c, "profile is not public", "Profile is not public", http.StatusForbidden)
	}

	if account.Prn == tokenOwner {
		profile.Email = account.Email
	}

	profile.Nick = account.Nick

	return echoutil.WriteJSON(c, http.StatusOK, profile)
}
