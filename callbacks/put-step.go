//
// Copyright (c) 2017-2023 Pantacor Ltd.
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

package callbacks

import (
	"context"
	"log"
	"strings"

	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/trails/trailmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// PublicStep is a structure of a public step
type PublicStep struct {
	StepID       string    `json:"step_id" bson:"step_id"`
	Owner        string    `json:"owner"`
	DeviceID     string    `json:"device_id" bson:"device_id"`
	ObjectSha    []string  `bson:"object_sha" json:"object_sha"`
	IsPublic     bool      `json:"public" bson:"ispublic"`
	Garbage      bool      `json:"garbage" bson:"garbage"`
	TimeModified time.Time `json:"timemodified" bson:"timemodified"`
	CreatedAt    time.Time `json:"created_at" bson:"created_at"`
	UpdatedAt    time.Time `json:"updated_at" bson:"updated_at"`
}

// handlePutStep Callback api for step changes
// @Summary Callback api for step changes
// @Description Callback api for step changes
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param id path string true "ID"
// @Success 200 {object} PublicStep
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /callbacks/steps/{id} [put]
func (a *App) handlePutStep(w rest.ResponseWriter, r *rest.Request) {
	var step trailmodels.Step
	stepID := r.PathParam("id")

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	err := collection.FindOne(ctx,
		bson.M{
			"_id": stepID,
		}).Decode(&step)
	if err == mongo.ErrNoDocuments {
		utils.RestErrorWrapper(w, "Not Found", http.StatusNotFound)
		return
	} else if err != nil {
		log.Print(err.Error())
		utils.RestErrorWrapper(w, "Internal Error:"+err.Error(), http.StatusInternalServerError)
		return
	}

	var publicStep PublicStep
	var hasPublicStep bool

	err = a.FindPublicStep(r.Context(), step.ID, &publicStep)
	if err == nil {
		hasPublicStep = true
	} else if err == mongo.ErrNoDocuments {
		hasPublicStep = false
	} else if err != nil {
		utils.RestErrorWrapper(w, err.Error(), http.StatusForbidden)
		return
	}

	timeModifiedStr, ok := r.URL.Query()["timemodified"]
	if ok {
		timeModified, err := time.Parse(time.RFC3339Nano, timeModifiedStr[0])
		if err != nil {
			utils.RestErrorWrapper(w, "Error Parsing timemodified:"+err.Error(), http.StatusForbidden)
			return
		}
		if hasPublicStep && publicStep.TimeModified.After(timeModified) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}

	err = a.SavePublicStep(r.Context(), &step, &publicStep)
	if err != nil {
		utils.RestErrorWrapper(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Mark the flag "mark_public_processed" as TRUE
	err = a.MarkStepAsProcessed(r.Context(), step.ID)
	if err != nil {
		utils.RestErrorWrapper(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteJson(publicStep)
}

// SavePublicStep is used to save public step
func (a *App) SavePublicStep(ctx context.Context, step *trailmodels.Step, publicStep *PublicStep) error {

	if publicStep.StepID == "" {
		publicStep.CreatedAt = time.Now()
	}
	publicStep.UpdatedAt = time.Now()

	publicStep.StepID = step.ID
	publicStep.DeviceID = step.TrailID.Hex()
	publicStep.Owner = step.Owner
	publicStep.IsPublic = step.IsPublic
	publicStep.Garbage = step.Garbage
	publicStep.TimeModified = step.TimeModified
	objectShaList, err := a.GetStepObjectShas(ctx, step)
	if err != nil {
		return err
	}
	publicStep.ObjectSha = objectShaList

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_public_steps")

	ctxC, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	updateOptions := options.Update()
	updateOptions.SetUpsert(true)

	_, err = collection.UpdateOne(ctxC,
		bson.M{"step_id": step.ID},
		bson.M{"$set": &publicStep},
		updateOptions)
	if err != nil {
		return err
	}
	return nil
}

// MarkStepAsProcessed is to mark step as processed
func (a *App) MarkStepAsProcessed(ctx context.Context, ID string) error {

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	ctxC, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	_, err := collection.UpdateOne(
		ctxC,
		bson.M{"_id": ID},
		bson.M{"$set": bson.M{
			"mark_public_processed": true,
		}},
		nil,
	)
	if err != nil {
		return err
	}
	return nil
}

// GetStepObjectShas is to get step object shas
func (a *App) GetStepObjectShas(ctx context.Context, step *trailmodels.Step) ([]string, error) {

	objectShaList := []string{}
	objMap := map[string]bool{}
	state := step.State

	if len(state) == 0 {
		return objectShaList, nil
	}

	for key, v := range state {
		sha, ok := v.(string)
		if !ok {
			continue
		}
		if strings.HasSuffix(key, ".json") && !utils.IsSha256HexString(sha) ||
			key == "#spec" {
			continue
		}
		if !utils.IsSha256HexString(sha) {
			log.Println("Bad JSON format: object sha not a sha: " + sha)
			continue
		}
		if _, ok := objMap[sha]; !ok {
			existsInDb, err := a.IsObjectExistsInDb(ctx, sha)
			if err != nil {
				return nil, err
			}
			if existsInDb {
				objectShaList = append(objectShaList, sha)
			} else {
				log.Println("Step " + step.ID + " state references object (" + sha + ") that does not exist in DB")
				continue
			}
		}
	}

	return objectShaList, nil
}

// FindPublicStep is to find public step
func (a *App) FindPublicStep(ctx context.Context, StepID string, publicStep *PublicStep) error {

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_public_steps")

	ctxC, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	err := collection.FindOne(ctxC, bson.M{
		"step_id": StepID,
	}).Decode(&publicStep)

	return err
}

// IsObjectExistsInDb is to check wether an  object exists in db or not
func (a *App) IsObjectExistsInDb(ctx context.Context, Sha string) (bool, error) {

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	ctxC, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	count, err := collection.CountDocuments(ctxC, bson.M{
		"id": Sha,
		"$or": []bson.M{
			{"linked_object": nil},
			{"linked_object": ""},
		},
	})
	return (count > 0), err
}
