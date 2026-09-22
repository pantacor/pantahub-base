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

// Package apps package to manage extensions of the oauth protocol
package apps

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	petname "github.com/dustinkirkland/golang-petname"
	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/auth/redirecturi"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
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
func (app *App) handleCreateApp(c *echo.Context) error {
	newApp := &TPApp{}
	payload := &CreateAppPayload{Logo: ""}
	if err := echoutil.DecodeJsonPayload(c, payload); err != nil {
		return echoutil.RestErrorWrapperUser(c, err.Error(), "invalid request body", http.StatusBadRequest)
	}

	var owner interface{}
	var ownerNick interface{}
	jwtPayload, ok := echoutil.Lookup(c, echoutil.KeyJWTPayload)
	if ok {
		owner, ok = jwtPayload.(jwtgo.MapClaims)["prn"]
		ownerNick, ok = jwtPayload.(jwtgo.MapClaims)["nick"]
	} else {
		return echoutil.RestErrorWrapper(c, "Owner can't be defined", http.StatusBadRequest)
	}

	err := validatePayload(payload)
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, err.Error(), err.Error(), http.StatusBadRequest)
	}

	mgoid := bson.NewObjectId()
	ObjectID, err := primitive.ObjectIDFromHex(mgoid.Hex())
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
	}

	apptype, err := parseType(payload.Type)
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, err.Error(), err.Error(), http.StatusBadRequest)
	}

	if payload.Nick == "" {
		payload.Nick = petname.Generate(2, "_")
	}
	if reservedNick(payload.Nick) {
		return echoutil.RestErrorWrapperUser(c, "nick is reserved for a built-in client", "nick is reserved for a built-in client", http.StatusConflict)
	}

	scopes, err := parseScopes(payload.Scopes, payload.Nick)
	if err != nil {
		return echoutil.RestErrorWrapperUser(c, err.Error(), err.Error(), http.StatusBadRequest)
	}

	if apptype == AppTypeConfidential {
		newApp.Secret, err = utils.GenerateSecret(30)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Error generating secret", http.StatusInternalServerError)
		}
		newApp.SecretHash = utils.HashSecret(newApp.Secret)
	}

	if apptype == AppTypeConfidential && len(payload.ExposedScopes) > 0 {
		newApp.ExposedScopes, err = parseScopes(payload.ExposedScopes, payload.Nick)
		if err != nil {
			return echoutil.RestErrorWrapperUser(c, err.Error(), err.Error(), http.StatusBadRequest)
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

	_, err = CreateOrUpdateApp(c.Request().Context(), newApp, app.mongoClient.Database(utils.MongoDb))
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error creating third party application "+err.Error(), http.StatusInternalServerError)
	}

	return echoutil.WriteJSON(c, http.StatusOK, newApp)
}

func parseType(typeofApp string) (string, error) {
	switch typeofApp {
	case AppTypePublic:
		return AppTypePublic, nil
	case AppTypeConfidential:
		return AppTypeConfidential, nil
	case AppTypePKCE:
		return AppTypePKCE, nil
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
	for _, uri := range app.RedirectURIs {
		if err := redirecturi.ValidateURI(uri); err != nil {
			return errors.New("invalid redirect URI '" + uri + "': " + err.Error())
		}
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
