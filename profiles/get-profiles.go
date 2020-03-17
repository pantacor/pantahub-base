//
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

package profiles

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo/options"

	"gopkg.in/mgo.v2/bson"
)

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
// @Success 200 {array} Profile
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /profiles [get]
func (a *App) handleGetProfiles(w rest.ResponseWriter, r *rest.Request) {
	_, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		err := devices.ModelError{}
		err.Code = http.StatusInternalServerError
		err.Message = "You need to be logged in as a USER"

		w.WriteHeader(int(err.Code))
		w.WriteJson(err)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	profiles := make([]Profile, 0)

	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	query := bson.M{}

	limit := int64(20) //Default page size=20
	skip := int64(0)

	err := errors.New("")

	value, ok := r.URL.Query()["limit"]
	if ok {
		limit, err = strconv.ParseInt(value[0], 10, 64)
		if err != nil {
			panic(err)
		}
	}
	value, ok = r.URL.Query()["page"]
	if ok {
		page, err := strconv.ParseInt(value[0], 10, 64)
		if err != nil {
			panic(err)
		}
		skip = page * limit
	}

	findOptions.SetLimit(limit)
	findOptions.SetSkip(skip)

	for k, v := range r.URL.Query() {
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

	cur, err := collection.Find(ctx, query, findOptions)
	if err != nil {
		utils.RestErrorWrapper(w, "Error on fetching accounts:"+err.Error(), http.StatusForbidden)
		return
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := accounts.Account{}
		err := cur.Decode(&result)
		if err != nil {
			utils.RestErrorWrapper(w, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
			return
		}

		havePublicDevices, err := a.HavePublicDevices(result.ID)
		if err != nil {
			utils.RestErrorWrapper(w, err.Error(), http.StatusForbidden)
			return
		}

		profile := Profile{}
		if havePublicDevices && result.Nick != "" {

			profile.ID = result.ID
			profile.Nick = result.Nick

			profiles = append(profiles, profile)
		}
	}

	w.WriteJson(profiles)
}
