//
// Copyright 2016-2020  Pantacor Ltd.
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

// Package trails offer a two party master/slave relationship enabling
// the master to asynchronously deploy configuration changes to its
// slave in a stepwise manner.
//
// ## Trail API Overview
//
// A trail represents a RESTful device state management endpoint optimized for
// high latency, asynchronous configuration management as found in the problem
// space of management of edge compute device world.
//
//  XXX: add proper API high level doc here (deleted outdated content)
//       handler func inline doc should stay up to date though...
//
// Detailed documentation for the various operations on the API endpoints can be
// at the handler functions below.
//
// TODOs:
//   - properly document trails API once finalized
//   - abstract access control in a better managable manner and less
//     mistake/oversight likely manner (probably descriptive/configuration style)
//   - ensure step and progres time can be effectively read from trail meta info
//     probably async/delayed update to ensure scalability (e.g. once every 5
//     minute if there has been any step touch we update last progress etc
//   - ensure that devices can query steps that need enqueing efficiently
//   - enusre that in-sync time and status is timely updated based on step and
//     progress
//   - find smart way to figure when device is in sync based on reported state
//   - consider enforcing sequential processing of steps to have a clean tail?
package trails

import (
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"context"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"gopkg.in/mgo.v2/bson"
)

// PvrRemote pvr remote specification payload
type PvrRemote struct {
	RemoteSpec         string   `json:"pvr-spec"`         // the pvr remote protocol spec available
	JSONGetURL         string   `json:"json-get-url"`     // where to pvr post stuff
	JSONKey            string   `json:"json-key"`         // what key is to use in post json [default: json]
	ObjectsEndpointURL string   `json:"objects-endpoint"` // where to store/retrieve objects
	PostURL            string   `json:"post-url"`         // where to post/announce new revisions
	PostFields         []string `json:"post-fields"`      // what fields require input
	PostFieldsOpt      []string `json:"post-fields-opt"`  // what optional fields are available [default: <empty>]
}

// App trails rest application
type App struct {
	jwtMiddleware *jwt.JWTMiddleware
	API           *rest.Api
	mongoClient   *mongo.Client
}

// Trail define the structure of a trail
type Trail struct {
	ID     primitive.ObjectID `json:"id" bson:"_id"`
	Owner  string             `json:"owner"`
	Device string             `json:"device"`
	//  Admins   []string `json:"admins"`   // XXX: maybe this is best way to do delegating device access....
	LastInSync   time.Time              `json:"last-insync" bson:"last-insync"`
	LastTouched  time.Time              `json:"last-touched" bson:"last-touched"`
	FactoryState map[string]interface{} `json:"factory-state" bson:"factory-state"`
	UsedObjects  []string               `bson:"used_objects" json:"used_objects"`
}

// Step wanted can be added by the device owner or delegate.
// steps that were not reported can be deleted still. other steps
// cannot be deleted until the device gets deleted as well.
type Step struct {
	ID                  string                 `json:"id" bson:"_id"` // XXX: make type
	Owner               string                 `json:"owner"`
	Device              string                 `json:"device"`
	Committer           string                 `json:"committer"`
	TrailID             primitive.ObjectID     `json:"trail-id" bson:"trail-id"` //parent id
	Rev                 int                    `json:"rev"`
	CommitMsg           string                 `json:"commit-msg" bson:"commit-msg"`
	State               map[string]interface{} `json:"state"` // json blurb
	StateSha            string                 `json:"state-sha" bson:"statesha"`
	StepProgress        StepProgress           `json:"progress" bson:"progress"`
	StepTime            time.Time              `json:"step-time" bson:"step-time"`
	ProgressTime        time.Time              `json:"progress-time" bson:"progress-time"`
	Meta                map[string]interface{} `json:"meta"` // json blurb
	UsedObjects         []string               `bson:"used_objects" json:"used_objects"`
	IsPublic            bool                   `json:"-" bson:"ispublic"`
	MarkPublicProcessed bool                   `json:"mark_public_processed" bson:"mark_public_processed"`
	Garbage             bool                   `json:"garbage" bson:"garbage"`
	TimeCreated         time.Time              `json:"time-created" bson:"timecreated"`
	TimeModified        time.Time              `json:"time-modified" bson:"timemodified"`
}

// StepProgress progression of a step
type StepProgress struct {
	Progress  int              `json:"progress"`                    // progress number. steps or 1-100
	Downloads DownloadProgress `json:"downloads" bson:"downloads"`  // progress number. steps or 1-100
	StatusMsg string           `json:"status-msg" bson:"statusmsg"` // message of progress status
	Data      interface{}      `json:"data,omitempty" bson:"data"`  // data field that can hold things the device wants to remember
	Status    string           `json:"status"`                      // status code
	Log       string           `json:"log"`                         // log if available
}

// DownloadProgress holds info about total and individual download progress
type DownloadProgress struct {
	Total   ObjectProgress   `json:"total" bson:"total"`
	Objects []ObjectProgress `json:"objects" bson:"objects"`
}

// ObjectProgress holds info object download progress
type ObjectProgress struct {
	ObjectName      string `json:"object_name,omitempty" bson:"object_name,omitempty"`
	ObjectID        string `json:"object_id,omitempty" bson:"object_id,omitempty"`
	TotalSize       int64  `json:"total_size" bson:"total_size"`
	StartTime       int64  `json:"start_time" bson:"start_time"`
	CurrentTime     int64  `json:"current_time" bson:"currentb_time"`
	TotalDownloaded int64  `json:"total_downloaded" bson:"total_downloaded"`
}

// TrailSummary details about a trail
type TrailSummary struct {
	DeviceID         string    `json:"deviceid" bson:"deviceid"`
	Device           string    `json:"device" bson:"device"`
	DeviceNick       string    `json:"device-nick" bson:"device_nick"`
	Rev              int       `json:"revision" bson:"revision"`
	ProgressRev      int       `json:"progress-revision" bson:"progress_revision"`
	Progress         int       `json:"progress" bson:"progress"` // progress number. steps or 1-100
	IsPublic         bool      `json:"public" bson:"public"`
	StateSha         string    `json:"state-sha" bson:"state_sha256"`
	StatusMsg        string    `json:"status-msg" bson:"status_msg"` // message of progress status
	Status           string    `json:"status" bson:"status"`         // status code
	Timestamp        time.Time `json:"timestamp" bson:"timestamp"`   // greater of last seen and last modified
	StepTime         time.Time `json:"step-time" bson:"step_time"`
	ProgressTime     time.Time `json:"progress-time" bson:"progress_time"`
	TrailTouchedTime time.Time `json:"trail-touched-time" bson:"trail_touched_time"`
	RealIP           string    `json:"real-ip" bson:"real_ip"`
	FleetGroup       string    `json:"fleet-group" bson:"fleet_group"`
	FleetModel       string    `json:"fleet-model" bson:"fleet_model"`
	FleetLocation    string    `json:"fleet-location" bson:"fleet_location"`
	FleetRev         string    `json:"fleet-rev" bson:"fleet_rev"`
	Owner            string    `json:"-" bson:"owner"`
}

func handleAuth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

// XXX: no product without fixing this to only parse ids that belong to this
// service instance
func prnGetID(prn string) string {
	idx := strings.Index(prn, "/")
	return prn[idx+1:]
}

func (a *App) getLatestStePrev(trailID primitive.ObjectID) (int, error) {
	collSteps := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if collSteps == nil {
		return -1, errors.New("bad database connetivity")
	}

	step := &Step{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	findOneOptions := options.FindOne()
	findOneOptions.SetSort(bson.M{"rev": -1})

	err := collSteps.FindOne(ctx, bson.M{
		"trail-id": trailID,
		"garbage":  bson.M{"$ne": true},
	}, findOneOptions).
		Decode(&step)

	if err != nil {
		return -1, err
	}
	if step == nil {
		return -1, errors.New("no step found for trail: " + trailID.Hex())
	}
	return step.Rev, err
}

func (a *App) handlePutStepsObject(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		utils.RestErrorWrapper(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	step := Step{}
	trailID := r.PathParam("id")
	rev := r.PathParam("rev")
	putID := r.PathParam("obj")

	if authType != "DEVICE" && authType != "USER" && authType != "SESSION" {
		utils.RestErrorWrapper(w, "Unknown AuthType", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := coll.FindOne(ctx, bson.M{
		"_id":     trailID + "-" + rev,
		"garbage": bson.M{"$ne": true},
	}).Decode(&step)
	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if authType == "DEVICE" && step.Device != owner {
		utils.RestErrorWrapper(w, "No access for device", http.StatusForbidden)
		return
	} else if (authType == "USER" || authType == "SESSION") && step.Owner != owner {
		utils.RestErrorWrapper(w, "No access for user/session", http.StatusForbidden)
		return
	}

	newObject := objects.Object{}
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	sha, err := utils.DecodeSha256HexString(putID)

	if err != nil {
		utils.RestErrorWrapper(w, "Put Trails Steps Object id must be a valid sha256", http.StatusBadRequest)
		return
	}

	storageID := objects.MakeStorageID(step.Owner, sha)

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = collection.FindOne(ctx, bson.M{
		"_id":     storageID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&newObject)

	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if newObject.Owner != step.Owner {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	nID := newObject.ID
	nOwner := newObject.Owner
	nStorageID := newObject.StorageID
	r.DecodeJsonPayload(&newObject)

	if newObject.ID != nID {
		utils.RestErrorWrapper(w, "Illegal Call Parameter Id", http.StatusConflict)
		return
	}
	if newObject.Owner != nOwner {
		utils.RestErrorWrapper(w, "Illegal Call Parameter Owner", http.StatusConflict)
		return
	}
	if newObject.StorageID != nStorageID {
		utils.RestErrorWrapper(w, "Illegal Call Parameter StorageId", http.StatusConflict)
		return
	}

	objects.SyncObjectSizes(&newObject)
	result, err := objects.CalcUsageAfterPut(newObject.Owner, a.mongoClient, newObject.ID, newObject.SizeInt)

	if err != nil {
		log.Println("Error to calc diskquota: " + err.Error())
		utils.RestErrorWrapper(w, "Error posting object", http.StatusInternalServerError)
		return
	}

	quota, err := objects.GetDiskQuota(newObject.Owner)

	if err != nil {
		log.Println("Error get diskquota setting: " + err.Error())
		utils.RestErrorWrapper(w, "Error to calc quota", http.StatusInternalServerError)
		return
	}

	if result.Total > quota {
		utils.RestErrorWrapperUser(
			w,
			err.Error(),
			"Quota exceeded; delete some objects or request a quota bump from team@pantahub.com",
			http.StatusPreconditionFailed)
	}

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	updateResult, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": storageID},
		bson.M{"$set": newObject},
		updateOptions,
	)
	if updateResult.MatchedCount == 0 {
		w.WriteHeader(http.StatusConflict)
		w.Header().Add("X-PH-Error", "Error inserting object into database ")
	}
	if err != nil {
		w.WriteHeader(http.StatusConflict)
		w.Header().Add("X-PH-Error", "Error inserting object into database "+err.Error())
	}

	issuerURL := utils.GetAPIEndpoint("/trails")
	newObjectWithAccess := objects.MakeObjAccessible(issuerURL, newObject.Owner, newObject, storageID)
	w.WriteJson(newObjectWithAccess)
}

// ProcessObjectsInState :
/*
1.Get Object List from the State field
2.UnMark All Objects As Garbages if they are marked as garbage
*/
func ProcessObjectsInState(
	owner string,
	state map[string]interface{},
	autoLink bool,
	a *App,
) (
	objects []string,
	err error,
) {
	objectList, err := GetStateObjects(owner, state, autoLink, a)
	if err != nil {
		return objectList, err
	}
	err = RestoreObjects(objectList, a)
	if err != nil {
		return objectList, err
	}
	return objectList, nil
}

// GetStateObjects : Get State Objects
func GetStateObjects(
	owner string,
	state map[string]interface{},
	autoLink bool,
	a *App,
) (
	[]string,
	error,
) {
	objectList := []string{}
	objMap := map[string]bool{}
	if len(state) == 0 {
		return objectList, nil
	}

	spec, ok := state["#spec"]
	if !ok {
		return nil, errors.New("state_object: Invalid state:#spec is missing")
	}

	specValue, ok := spec.(string)
	if !ok {
		return nil, errors.New("state_object: Invalid state:Value of #spec should be string")
	}

	if specValue != "pantavisor-multi-platform@1" &&
		specValue != "pantavisor-service-system@1" {
		return nil, errors.New("state_object: Invalid state:Value of #spec should not be " + specValue)
	}

	objectsApp := objects.Build(a.mongoClient)

	for key, v := range state {
		if strings.HasSuffix(key, ".json") ||
			key == "#spec" {
			continue
		}
		sha, found := v.(string)
		if !found {
			return nil, errors.New("state_object: Object is not a string[sha:" + sha + "]")
		}

		object, err := objectsApp.ResolveObjectWithLinks(owner, sha, autoLink)

		if err != nil {
			return nil, err
		}

		// Save object
		err = objectsApp.SaveObject(object, false)
		if err != nil {
			return nil, errors.New("Error saving object:" + err.Error())
		}

		if _, ok := objMap[object.StorageID]; !ok {
			objectList = append(objectList, object.StorageID)
		}
	}
	return objectList, nil
}

// RestoreObjects : Takes the list of objects and unmarks them garbage.
func RestoreObjects(
	objectList []string,
	a *App,
) error {

	for _, storageSha := range objectList {

		result, err := IsObjectGarbage(storageSha, a)
		if err != nil {
			return errors.New("Error checking garbage object:" + err.Error() + "[sha:" + storageSha + "]")
		}
		if result {
			err := UnMarkObjectAsGarbage(storageSha, a)
			if err != nil {
				return errors.New("Error unmarking object as garbage:" + err.Error() + "[sha:" + storageSha + "]")
			}
		}
	}
	return nil
}

// IsObjectGarbage : to check if an object is garbage or not
func IsObjectGarbage(ObjectID string, a *App) (
	bool,
	error,
) {
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	objectCount, err := collection.CountDocuments(ctx,
		bson.M{
			"_id":     ObjectID,
			"garbage": true,
		},
	)
	if err != nil {
		return false, errors.New("Error Finding Object:" + err.Error())
	}
	return (objectCount == 1), nil
}

// UnMarkObjectAsGarbage : to unmark object as garbage
func UnMarkObjectAsGarbage(ObjectID string, a *App) error {
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updateResult, err := collection.UpdateOne(
		ctx,
		bson.M{
			"_id": ObjectID,
		},
		bson.M{"$set": bson.M{
			"garbage": false,
		}},
	)
	if updateResult.MatchedCount == 0 {
		return errors.New("unmark_object_as_garbage:Error updating object: not found")
	}
	if err != nil {
		return errors.New("unmark_object_as_garbage:Error updating object:" + err.Error())
	}
	return nil
}

// IsDevicePublic checks if a device is public or not
func (a *App) IsDevicePublic(ID primitive.ObjectID) (bool, error) {

	devicesApp := devices.Build(a.mongoClient)
	device := devices.Device{}

	err := devicesApp.FindDeviceByID(ID, &device)
	if err != nil {
		return false, err
	}
	return device.IsPublic, nil
}
