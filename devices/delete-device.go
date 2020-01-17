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

package devices

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/mgo.v2/bson"
)

// handleDeleteDevice Mark a device to be deleted by device garbage collector
// @Summary Mark a device to be deleted by device garbage collector
// @Description Mark a device to be deleted by device garbage collector
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Param id path string true "ID|PRN|NICK"
// @Success 200 {array} Device
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/{id} [delete]
func (a *App) handleDeleteDevice(w rest.ResponseWriter, r *rest.Request) {
	delID := r.PathParam("id")

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in as a USER", http.StatusForbidden)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	device := Device{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deviceObjectID, err := primitive.ObjectIDFromHex(delID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	err = collection.FindOne(ctx, bson.M{
		"_id":     deviceObjectID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&device)
	if err != nil {
		if err != mongo.ErrNoDocuments {
			log.Println("Error deleting device: " + err.Error())
			utils.RestErrorWrapper(w, "Device not found", http.StatusInternalServerError)
			return
		}

		device.ID = deviceObjectID
		w.WriteJson(device)
		return
	}

	if device.Owner == owner {
		result, res := MarkDeviceAsGarbage(w, delID)
		if res.StatusCode() != 200 {
			log.Print(res)
			log.Print(result)
			utils.RestErrorWrapper(w, "Error calling GC API for Marking Device Garbage", http.StatusInternalServerError)
			return
		}
		if result.Status == 1 {
			device.Garbage = true
		}
	}

	w.WriteJson(device)
}
