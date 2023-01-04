// Copyright 2021  Pantacor Ltd.
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

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// handlePutGlobalMeta Get user profile global meta
// @Summary Get user profile global meta
// @Description Get user profile global meta
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags user
// @Param body body map[string]interface{} true "Global meta"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /profiles/metas [put]
func (a *App) handlePutGlobalMeta(w rest.ResponseWriter, r *rest.Request) {
	metas := map[string]interface{}{}
	r.DecodeJsonPayload(&metas)
	globalMeta := utils.BsonQuoteMap(&metas)

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if !ok {
		utils.RestErrorWrapper(w, "Token owner can't be defined", http.StatusInternalServerError)
		return
	}
	tokenOwner, ok := jwtPayload.(jwtgo.MapClaims)["prn"].(string)
	if !ok {
		utils.RestErrorWrapper(w, "Token owner can't be defined", http.StatusInternalServerError)
		return
	}

	account, err := a.getUserAccount(r.Context(), tokenOwner, "prn")
	if err != nil {
		switch err.(type) {
		default:
			utils.RestErrorWrapper(w, "Account "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	haveProfile, err := a.ExistsInProfiles(r.Context(), account.ID)
	if err != nil {
		utils.RestErrorWrapper(w, err.Error(), http.StatusForbidden)
		return
	}

	if !haveProfile {
		_, err := a.MakeUserProfile(r.Context(), account, nil)
		if err != nil {
			utils.RestErrorWrapper(w, err.Error(), http.StatusForbidden)
			return
		}
	}

	if account.Prn != tokenOwner {
		utils.RestErrorWrapperUser(w, err.Error(), "Only owners can write profile", http.StatusForbidden)
		return
	}

	profile, err := a.updateProfileMeta(r.Context(), account.Prn, globalMeta)
	if err != nil {
		utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
		return
	}

	w.WriteJson(utils.BsonUnquoteMap(&profile.Meta))
}
