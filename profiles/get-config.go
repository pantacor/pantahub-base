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
func (a *App) handleGetGlobalMeta(w rest.ResponseWriter, r *rest.Request) {
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
		utils.RestErrorWrapper(w, "Account "+err.Error(), http.StatusInternalServerError)
		return
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

	profile, err := a.getProfile(r.Context(), account.Prn, bson.M{"meta": 1})
	if err != nil {
		utils.RestErrorWrapper(w, "No Access", http.StatusForbidden)
		return
	}

	if account.Prn != tokenOwner {
		utils.RestErrorWrapperUser(w, err.Error(), "Profile is not public", http.StatusForbidden)
		return
	}

	w.WriteJson(utils.BsonUnquoteMap(&profile.Meta))
}
