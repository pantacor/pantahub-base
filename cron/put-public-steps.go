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

package cron

import (
	"context"

	"net/http"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/callbacks"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
)

// handlePutSteps Api to process all public steps
// @Summary Api to process all public steps
// @Description Api to process all public steps
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags steps
// @Param id path string true "ID|Nick|PRN"
// @Success 200 {array} callbacks.PublicStep
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /cron/steps [put]
func (a *App) handlePutSteps(c *echo.Context) error {

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")
	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}
	callbackApp := callbacks.Build(a.mongoClient)

	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	ctx, cancel := context.WithTimeout(c.Request().Context(), a.CronJobTimeout)
	defer cancel()
	query := bson.M{
		"ispublic":              true,
		"mark_public_processed": bson.M{"$ne": true},
	}
	cur, err := collection.Find(ctx, query, findOptions)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error on fetching public steps:"+err.Error(), http.StatusForbidden)
	}
	defer cur.Close(ctx)

	response := []callbacks.PublicStep{}

	for cur.Next(ctx) {
		step := trailmodels.Step{}
		err := cur.Decode(&step)
		if err != nil {
			return echoutil.RestErrorWrapper(c, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
		}

		var publicStep callbacks.PublicStep

		err = callbackApp.FindPublicStep(c.Request().Context(), step.ID, &publicStep)
		if err != nil && err != mongo.ErrNoDocuments {
			return echoutil.RestErrorWrapper(c, err.Error(), http.StatusForbidden)
		}

		err = callbackApp.SavePublicStep(c.Request().Context(), &step, &publicStep)
		if err != nil {
			return echoutil.RestErrorWrapper(c, err.Error(), http.StatusForbidden)
		}

		// Mark the flag "mark_public_processed" as TRUE
		err = callbackApp.MarkStepAsProcessed(c.Request().Context(), step.ID)
		if err != nil {
			return echoutil.RestErrorWrapper(c, err.Error(), http.StatusBadRequest)
		}

		response = append(response, publicStep)
	}

	return echoutil.WriteJSON(c, http.StatusOK, response)
}
