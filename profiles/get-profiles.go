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

package profiles

import (
	"context"
	jwtgo "github.com/golang-jwt/jwt/v5"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/mongo/options"

	"gopkg.in/mgo.v2/bson"
)

// ModelError error type
type ModelError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// handleGetProfiles Get all user profiles
// @Summary Get all user profiles
// page: You can use this param to navigate through different pages
// limit: You can this param to decide the page Size(default=20)
// nick: You can search nicks by using this param.(eg:GET /profiles/?nick=^abc)
// @Description Get all user profiles
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags profile
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /profiles/ [get]
func (a *App) handleGetProfiles(c *echo.Context) error {
	owner, ok := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"]
	if !ok {
		err := ModelError{}
		err.Code = http.StatusInternalServerError
		err.Message = "You need to be logged in as a USER"

		return echoutil.WriteJSON(c, int(err.Code), err)
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	profiles := make([]*Profile, 0)

	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)

	query := bson.M{}

	limit := int64(20) //Default page size=20
	skip := int64(0)

	value, ok := c.Request().URL.Query()["limit"]
	if ok {
		var err error
		limit, err = strconv.ParseInt(value[0], 10, 64)
		if err != nil {
			panic(err)
		}
	}
	value, ok = c.Request().URL.Query()["page"]
	if ok {
		page, err := strconv.ParseInt(value[0], 10, 64)
		if err != nil {
			panic(err)
		}
		skip = page * limit
	}

	findOptions.SetLimit(limit)
	findOptions.SetSkip(skip)

	for k, v := range c.Request().URL.Query() {
		if k == "page" || k == "limit" {
			continue
		}
		if query[k] == nil && k == "nick" {
			if strings.HasPrefix(v[0], "!") {
				v[0] = strings.TrimPrefix(v[0], "!")
				query[k] = bson.M{"$ne": v[0]}
			} else if strings.HasPrefix(v[0], "^") {
				v[0] = strings.TrimPrefix(v[0], "^")
				query[k] = bson.M{"$regex": "^" + v[0], "$options": "i"}
			} else {
				query[k] = v[0]
			}
		}
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	cur, err := collection.Find(ctx, query, findOptions)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error on fetching accounts:"+err.Error(), http.StatusForbidden)
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := accounts.Account{}
		err := cur.Decode(&result)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
		}

		havePublicDevices, err := a.HavePublicDevices(c.Request().Context(), result.Prn)
		if err != nil {
			return echoutil.RestErrorWrapper(c, err.Error(), http.StatusForbidden)
		}

		profile, _ := a.getProfile(c.Request().Context(), result.Prn, nil)
		if (havePublicDevices || result.Prn == owner.(string)) && result.Nick != "" {
			profile.Nick = result.Nick
			profiles = append(profiles, profile)
		}
	}

	return echoutil.WriteJSON(c, http.StatusOK, profiles)
}
