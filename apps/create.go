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

// Package apps package to manage extensions of the oauth protocol
package apps

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	petname "github.com/dustinkirkland/golang-petname"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"gopkg.in/mgo.v2/bson"
)

// CreateAppPayload create app json payload
type CreateAppPayload struct {
	Type          string        `json:"type"`
	Nick          string        `json:"nick"`
	Name          string        `json:"name"`
	Logo          string        `json:"logo"`
	RedirectURIs  []string      `json:"redirect_uris,omitempty"`
	Scopes        []utils.Scope `json:"scopes,omitempty"`
	ExposedScopes []utils.Scope `json:"exposed_scopes,omitempty" bson:"exposed_scopes,omitempty"`
}

// handleCreateApp create a new oauth client
// @Summary Create a new third party application
// @Description This define a new application to be used as OAuth client
// @Accept  json
// @Produce  json
// @Tags apps
// @Security ApiKeyAuth
// @Param body body CreateAppPayload true "Create app body"
// @Success 200 {object} TPApp
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /apps/ [post]
func (app *App) handleCreateApp(w rest.ResponseWriter, r *rest.Request) {
	newApp := &TPApp{}
	payload := &CreateAppPayload{Logo: ""}
	r.DecodeJsonPayload(payload)

	var owner interface{}
	var ownerNick interface{}
	jwtPayload, ok := r.Env["JWT_PAYLOAD"]
	if ok {
		owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
		ownerNick, ok = jwtPayload.(jwtgo.MapClaims)["nick"]
	} else {
		utils.RestErrorWrapper(w, "Owner can't be defined", http.StatusBadRequest)
		return
	}

	err := validatePayload(payload)
	if err != nil {
		utils.RestErrorWrapper(w, err.Error(), http.StatusBadRequest)
		return
	}

	mgoid := bson.NewObjectId()
	ObjectID, err := primitive.ObjectIDFromHex(mgoid.Hex())
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}

	apptype, err := parseType(payload.Type)
	if err != nil {
		utils.RestErrorWrapper(w, err.Error(), http.StatusBadRequest)
		return
	}

	if payload.Nick == "" {
		payload.Nick = petname.Generate(2, "_")
	}

	scopes, err := parseScopes(payload.Scopes, payload.Nick)
	if err != nil {
		utils.RestErrorWrapper(w, err.Error(), http.StatusBadRequest)
		return
	}

	if apptype == AppTypeConfidential {
		newApp.Secret, err = utils.GenerateSecret(30)
		if err != nil {
			utils.RestErrorWrapper(w, "Error generating secret", http.StatusInternalServerError)
			return
		}
	}

	if apptype == AppTypeConfidential && len(payload.ExposedScopes) > 0 {
		newApp.ExposedScopes, err = parseScopes(payload.ExposedScopes, payload.Nick)
		if err != nil {
			utils.RestErrorWrapper(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	newApp.ID = ObjectID
	newApp.Type = apptype
	newApp.Scopes = scopes
	newApp.Name = payload.Name
	newApp.Logo = payload.Logo
	newApp.Prn = utils.BuildScopePrn(payload.Nick)
	newApp.Nick = payload.Nick
	newApp.RedirectURIs = payload.RedirectURIs
	newApp.Owner = owner.(string)
	newApp.OwnerNick = ownerNick.(string)
	newApp.TimeCreated = time.Now()
	newApp.TimeModified = newApp.TimeCreated
	newApp.DeletedAt = nil

	_, err = CreateOrUpdateApp(r.Context(), newApp, app.mongoClient.Database(utils.MongoDb))
	if err != nil {
		utils.RestErrorWrapper(w, "Error creating third party application "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteJson(newApp)
}

func parseType(typeofApp string) (string, error) {
	switch typeofApp {
	case AppTypePublic:
		return AppTypePublic, nil
	case AppTypeConfidential:
		return AppTypeConfidential, nil
	default:
		return "", errors.New("Invalid app type")
	}
}

func parseScopes(scopes []utils.Scope, serviceID string) ([]utils.Scope, error) {
	newScopes := []utils.Scope{}
	servicePrn := utils.BuildScopePrn(serviceID)
	for _, scope := range scopes {
		if !isEmpty(scope) {
			phScope := matchPantahubScope(scope)
			if phScope != nil {
				phScope.Required = scope.Required
				newScopes = append(newScopes, *phScope)
				continue
			}
			if (scope.Service == "" || scope.Service == servicePrn) && phScope == nil {
				newScopes = append(newScopes, utils.Scope{
					ID:          scope.ID,
					Service:     servicePrn,
					Description: scope.Description,
					Required:    scope.Required,
				})
				continue
			}
			newScopes = append(newScopes, scope)
		}
	}

	if len(newScopes) == 0 {
		return newScopes, errors.New("Scopes are invalid")
	}

	return newScopes, nil
}

func matchPantahubScope(scope utils.Scope) *utils.Scope {
	phScope := utils.PhScopesMap[scope.ID]
	if isEmpty(phScope) {
		return nil
	}
	if phScope.Service != scope.Service {
		return nil
	}

	return &phScope
}

func isEmpty(scope utils.Scope) bool {
	return scope.ID == ""
}

func validatePayload(app *CreateAppPayload) error {
	if app.Type == "" {
		return errors.New("App type must be defined")
	}
	if len(app.Scopes) == 0 {
		return errors.New("A new app need to have at least one scope")
	}
	if len(app.RedirectURIs) == 0 {
		return errors.New("A new app need to have at least one redirect URI")
	}

	logoSize := utils.CalcBinarySize(app.Logo)
	logoMaxSizeStr := utils.GetEnv(utils.EnvPantahub3rdAppLogoMaxSizeKb)
	logoMaxSize, err := strconv.Atoi(logoMaxSizeStr)
	if err != nil {
		return err
	}

	if logoSize >= (logoMaxSize * 1024) {
		return errors.New("Application logo can't be greater than " + logoMaxSizeStr + "Kb")
	}

	return nil
}
