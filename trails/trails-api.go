//
// Copyright 2016-2018  Pantacor Ltd.
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
package trails

// Trails offer a two party master/slave relationship enabling
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
import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"context"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/metrics"
	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/storagedriver"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx"
	"gopkg.in/mgo.v2/bson"
)

type PvrRemote struct {
	RemoteSpec         string   `json:"pvr-spec"`         // the pvr remote protocol spec available
	JsonGetUrl         string   `json:"json-get-url"`     // where to pvr post stuff
	JsonKey            string   `json:"json-key"`         // what key is to use in post json [default: json]
	ObjectsEndpointUrl string   `json:"objects-endpoint"` // where to store/retrieve objects
	PostUrl            string   `json:"post-url"`         // where to post/announce new revisions
	PostFields         []string `json:"post-fields"`      // what fields require input
	PostFieldsOpt      []string `json:"post-fields-opt"`  // what optional fields are available [default: <empty>]
}

type TrailsApp struct {
	jwt_middleware *jwt.JWTMiddleware
	Api            *rest.Api
	mongoClient    *mongo.Client
}

type Trail struct {
	Id     primitive.ObjectID `json:"id" bson:"_id"`
	Owner  string             `json:"owner"`
	Device string             `json:"device"`
	//  Admins   []string `json:"admins"`   // XXX: maybe this is best way to do delegating device access....
	LastInSync   time.Time              `json:"last-insync" bson:"last-insync"`
	LastTouched  time.Time              `json:"last-touched" bson:"last-touched"`
	FactoryState map[string]interface{} `json:"factory-state" bson:"factory-state"`
	UsedObjects  []string               `bson:"used_objects" json:"used_objects"`
}

// step wanted can be added by the device owner or delegate.
// steps that were not reported can be deleted still. other steps
// cannot be deleted until the device gets deleted as well.
type Step struct {
	Id           string                 `json:"id" bson:"_id"` // XXX: make type
	Owner        string                 `json:"owner"`
	Device       string                 `json:"device"`
	Committer    string                 `json:"committer"`
	TrailId      primitive.ObjectID     `json:"trail-id" bson:"trail-id"` //parent id
	Rev          int                    `json:"rev"`
	CommitMsg    string                 `json:"commit-msg" bson:"commit-msg"`
	State        map[string]interface{} `json:"state"` // json blurb
	StateSha     string                 `json:"state-sha" bson:"statesha"`
	StepProgress StepProgress           `json:"progress" bson:"progress"`
	StepTime     time.Time              `json:"step-time" bson:"step-time"`
	ProgressTime time.Time              `json:"progress-time" bson:"progress-time"`
	Meta         map[string]interface{} `json:"meta"` // json blurb
	UsedObjects  []string               `bson:"used_objects" json:"used_objects"`
}

type StepProgress struct {
	Progress  int    `json:"progress"`                    // progress number. steps or 1-100
	StatusMsg string `json:"status-msg" bson:"statusmsg"` // message of progress status
	Status    string `json:"status"`                      // status code
	Log       string `json:"log"`                         // log if available
}

type TrailSummary struct {
	DeviceId         string    `json:"deviceid" bson:"deviceid"`
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

func handle_auth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

// XXX: no product without fixing this to only parse ids that belong to this
// service instance
func prnGetId(prn string) string {
	idx := strings.Index(prn, "/")
	return prn[idx+1:]
}

// ## POST /trails/
//   usually done by device on first log in. This
//   initiates the trail by using the reported state as stepwanted 0 and setting
//   the step 0 to be the POSTED JSON. Either device accounts or user accounts can
//   do this for devices owned, but there can always only be ONE trail per device.
func (a *TrailsApp) handle_posttrail(w rest.ResponseWriter, r *rest.Request) {

	initialState := map[string]interface{}{}

	r.DecodeJsonPayload(&initialState)

	device, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if authType != "DEVICE" {
		// XXX: find right error
		rest.Error(w, "You need to be logged in as a DEVICE to post new trails", http.StatusForbidden)
		return
	}

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["owner"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "Device needs an owner", http.StatusForbidden)
		return
	}
	deviceID := prnGetId(device.(string))

	// do we need tip/tail here? or is that always read-only?
	newTrail := Trail{}
	deviceObjectID, err := primitive.ObjectIDFromHex(deviceID)
	if err != nil {
		rest.Error(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	newTrail.Id = deviceObjectID
	newTrail.Owner = owner.(string)
	newTrail.Device = device.(string)
	newTrail.LastInSync = time.Time{}
	newTrail.LastTouched = newTrail.LastInSync
	objectList, err := ProcessObjectsInState(newTrail.Owner, initialState, a)
	if err != nil {
		rest.Error(w, "Error processing trail objects in factory-state:"+err.Error(), http.StatusInternalServerError)
		return
	}
	newTrail.UsedObjects = objectList
	newTrail.FactoryState = utils.BsonQuoteMap(&initialState)

	newStep := Step{}
	newStep.Id = newTrail.Id.Hex() + "-0"
	newStep.TrailId = newTrail.Id
	newStep.Rev = 0
	stateSha, err := utils.StateSha(&initialState)
	if err != nil {
		rest.Error(w, "Error calculating state sha"+err.Error(), http.StatusInternalServerError)
		return
	}
	newStep.StateSha = stateSha
	newStep.Owner = newTrail.Owner
	newStep.Device = newTrail.Device
	newStep.CommitMsg = "Factory State (rev 0)"
	newStep.StepTime = time.Now() // XXX this should be factory time not now
	newStep.ProgressTime = time.Now()
	newStep.StepProgress.Status = "DONE"
	newStep.Meta = map[string]interface{}{}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_trails")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	objectList, err = ProcessObjectsInState(newStep.Owner, initialState, a)
	if err != nil {
		rest.Error(w, "Error processing step objects in state"+err.Error(), http.StatusInternalServerError)
		return
	}
	newStep.UsedObjects = objectList
	newStep.State = utils.BsonQuoteMap(&initialState)

	// XXX: prototype: for production we need to prevent posting twice!!
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = collection.InsertOne(
		ctx,
		newTrail,
	)
	if err != nil {
		rest.Error(w, "Error inserting trail into database "+err.Error(), http.StatusInternalServerError)
		return
	}

	collection = a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = collection.InsertOne(
		ctx,
		newStep,
	)

	if err != nil {
		rest.Error(w, "Error inserting step into database "+err.Error(), http.StatusInternalServerError)
		return
	}

	newTrail.FactoryState = utils.BsonUnquoteMap(&newTrail.FactoryState)
	w.WriteJson(newTrail)
}

//
// ## GET /trails/
//   devices get a list of one and only one trail. users get trails for all the
//   devices they have trail control over (right now simplified for owner)
//
func (a *TrailsApp) handle_gettrails(w rest.ResponseWriter, r *rest.Request) {

	initialState := map[string]interface{}{}

	r.DecodeJsonPayload(&initialState)

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_trails")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	ownerField := ""
	if authType == "DEVICE" {
		ownerField = "device"
	} else if authType == "USER" {
		ownerField = "owner"
	}

	trails := make([]Trail, 0)

	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cur, err := coll.Find(ctx, bson.M{
		ownerField: owner,
		"garbage":  bson.M{"$ne": true},
	}, findOptions)
	if err != nil {
		rest.Error(w, "Error on fetching devices:"+err.Error(), http.StatusForbidden)
		return
	}
	defer cur.Close(ctx)

	for cur.Next(ctx) {
		result := Trail{}
		err := cur.Decode(&result)
		if err != nil {
			rest.Error(w, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
			return
		}
		result.FactoryState = utils.BsonUnquoteMap(&result.FactoryState)
		trails = append(trails, result)
	}

	if authType == "DEVICE" {
		if len(trails) > 1 {
			log.Println("WARNING: more than one trail in db for device - bad DB: " + owner.(string))
			trails = trails[0:1]
		}
	}
	w.WriteJson(trails)
}

//
// ## GET /trails/:tid
//   get one trail; owning devices and users with trail control for the device
//   can get a trail. If not found or if no access, NotFound status code is
//   returned (XXX: make that true)
//
func (a *TrailsApp) handle_gettrail(w rest.ResponseWriter, r *rest.Request) {

	var err error

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_trails")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	getId := r.PathParam("id")
	trail := Trail{}

	isPublic, err := a.isTrailPublic(getId)

	if err != nil {
		rest.Error(w, "Error getting public trail", http.StatusInternalServerError)
		return
	}
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	trailObjectID, err := primitive.ObjectIDFromHex(getId)
	if err != nil {
		rest.Error(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	if isPublic {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailObjectID,
			"garbage": bson.M{"$ne": true},
		}).Decode(&trail)
	} else if authType == "DEVICE" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailObjectID,
			"device":  owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&trail)
	} else if authType == "USER" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailObjectID,
			"owner":   owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&trail)
	}

	if err != nil {
		rest.Error(w, "No access to resource: "+err.Error(), http.StatusInternalServerError)
		return
	}

	trail.FactoryState = utils.BsonUnquoteMap(&trail.FactoryState)

	w.WriteJson(trail)
}

func (a *TrailsApp) handle_gettrailpvrinfo(w rest.ResponseWriter, r *rest.Request) {

	var err error

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	getId := r.PathParam("id")
	step := Step{}

	isPublic, err := a.isTrailPublic(getId)

	if err != nil {
		rest.Error(w, "Error getting trail public", http.StatusInternalServerError)
		return
	}
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	trailObjectID, err := primitive.ObjectIDFromHex(getId)
	if err != nil {
		rest.Error(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	findOneOptions := options.FindOne()
	findOneOptions.SetSort(bson.M{"rev": -1})
	//	get last step
	if isPublic {
		err = coll.FindOne(ctx, bson.M{
			"trail-id": trailObjectID,
			"garbage":  bson.M{"$ne": true},
		}, findOneOptions).Decode(&step)
	} else if authType == "DEVICE" {
		err = coll.FindOne(ctx, bson.M{
			"device":   owner,
			"trail-id": trailObjectID,
			"garbage":  bson.M{"$ne": true},
		}, findOneOptions).Decode(&step)
	} else if authType == "USER" {
		err = coll.FindOne(ctx, bson.M{
			"owner":    owner,
			"trail-id": trailObjectID,
			"garbage":  bson.M{"$ne": true},
		}, findOneOptions).Decode(&step)
	}

	if err == mongo.ErrNoDocuments {
		rest.Error(w, "No access to device trail "+trailObjectID.Hex(), http.StatusForbidden)
		return
	}

	if err != nil {
		rest.Error(w, "No access to resource: "+err.Error(), http.StatusInternalServerError)
		return
	}

	oe := utils.GetApiEndpoint("/trails/" + getId + "/steps/" + strconv.Itoa(step.Rev) + "/objects")
	jsonGet := utils.GetApiEndpoint("/trails/" + getId + "/steps/" + strconv.Itoa(step.Rev) + "/state")
	postUrl := utils.GetApiEndpoint("/trails/" + getId + "/steps")
	postFields := []string{"commit-msg"}
	postFieldsOpt := []string{"rev"}

	remoteInfo := PvrRemote{
		RemoteSpec:         "pvr-pantahub-1",
		JsonGetUrl:         jsonGet,
		ObjectsEndpointUrl: oe,
		JsonKey:            "state",
		PostUrl:            postUrl,
		PostFields:         postFields,
		PostFieldsOpt:      postFieldsOpt,
	}

	w.WriteJson(remoteInfo)
}

func (a *TrailsApp) handle_getsteppvrinfo(w rest.ResponseWriter, r *rest.Request) {

	var err error

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	getId := r.PathParam("id")
	revId := r.PathParam("rev")
	stepId := getId + "-" + revId
	step := Step{}

	isPublic, err := a.isTrailPublic(getId)

	if err != nil {
		rest.Error(w, "Error getting trail public", http.StatusInternalServerError)
		return
	}

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	//	get last step
	if isPublic {
		err = coll.FindOne(ctx, bson.M{
			"_id":     stepId,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)

	} else if authType == "DEVICE" {
		err = coll.FindOne(ctx, bson.M{
			"device":  owner,
			"_id":     stepId,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	} else if authType == "USER" {
		err = coll.FindOne(ctx, bson.M{
			"owner":   owner,
			"_id":     stepId,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	}

	if err == mongo.ErrNoDocuments {
		rest.Error(w, "No access to device step trail "+stepId, http.StatusForbidden)
		return
	}

	if err != nil {
		rest.Error(w, "No access to resource: "+err.Error(), http.StatusInternalServerError)
		return
	}

	oe := utils.GetApiEndpoint("/trails/" + getId + "/steps/" +
		revId + "/objects")

	jsonUrl := utils.GetApiEndpoint("/trails/" + getId + "/steps/" +
		revId + "/state")

	postUrl := utils.GetApiEndpoint("/trails/" + getId + "/steps")
	postFields := []string{"msg"}
	postFieldsOpt := []string{}

	remoteInfo := PvrRemote{
		RemoteSpec:         "pvr-pantahub-1",
		JsonGetUrl:         jsonUrl,
		ObjectsEndpointUrl: oe,
		JsonKey:            "state",
		PostUrl:            postUrl,
		PostFields:         postFields,
		PostFieldsOpt:      postFieldsOpt,
	}

	w.WriteJson(remoteInfo)
}

func (a *TrailsApp) get_latest_steprev(trailId primitive.ObjectID) (int, error) {
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
		"trail-id": trailId,
		"garbage":  bson.M{"$ne": true},
	}, findOneOptions).
		Decode(&step)

	if err != nil {
		return -1, err
	}
	if step == nil {
		return -1, errors.New("no step found for trail: " + trailId.Hex())
	}
	return step.Rev, err
}

//
// POST /trails/:id/steps
//  post a new step to the head of the trail. You must include the correct Rev
//  number that must exactly be one incremented from the previous rev numbers.
//  In case of conflict creation of steps one will get an error.
//  In the DB the ID will be composite of trails ID + Rev; this ensures that
//  it will be unique. Also no step will be added if the previous one does not
//  exist that. This will include completeness of the step rev sequence.
//
func (a *TrailsApp) handle_poststep(w rest.ResponseWriter, r *rest.Request) {

	var err error

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["owner"]

	// if not a device there wont be an owner; so we use the caller (aka prn)
	if !ok {
		owner, ok = r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
		if !ok {
			// XXX: find right error
			rest.Error(w, "You need to be logged in as user or device", http.StatusForbidden)
			return
		}
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	collTrails := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_trails")

	if collTrails == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	trailId := r.PathParam("id")
	trail := Trail{}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	trailObjectID, err := primitive.ObjectIDFromHex(trailId)
	if err != nil {
		rest.Error(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}

	if authType == "USER" || authType == "DEVICE" {
		err = collTrails.FindOne(ctx, bson.M{
			"_id":     trailObjectID,
			"garbage": bson.M{"$ne": true},
		}).Decode(&trail)
	} else {
		rest.Error(w, "Need to be logged in as USER to post trail steps", http.StatusForbidden)
		return
	}

	if err != nil {
		rest.Error(w, "No resource access possible", http.StatusInternalServerError)
		return
	}

	if trail.Owner != owner {
		rest.Error(w, "No access", http.StatusForbidden)
		return
	}

	collSteps := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if collSteps == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	newStep := Step{}
	previousStep := Step{}
	r.DecodeJsonPayload(&newStep)

	if newStep.Rev == -1 {
		trailObjectID, err := primitive.ObjectIDFromHex(trailId)
		if err != nil {
			rest.Error(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
			return
		}
		newStep.Rev, err = a.get_latest_steprev(trailObjectID)
		newStep.Rev += 1
	}

	if err != nil {
		rest.Error(w, "Error auto appending step 1 "+err.Error(), http.StatusInternalServerError)
		return
	}

	stepId := trailId + "-" + strconv.Itoa(newStep.Rev-1)
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = collSteps.FindOne(ctx, bson.M{
		"_id":     stepId,
		"garbage": bson.M{"$ne": true},
	}).Decode(&previousStep)

	if err != nil {
		// XXX: figure how to be better on error cases here...
		rest.Error(w, "No access to resource or bad step rev", http.StatusInternalServerError)
		return
	}

	// XXX: introduce step diffs here and store them precalced

	newStep.Id = trail.Id.Hex() + "-" + strconv.Itoa(newStep.Rev)
	newStep.Owner = trail.Owner
	newStep.Device = trail.Device
	newStep.StepProgress = StepProgress{
		Status: "NEW",
	}
	newStep.TrailId = trail.Id
	newStep.StepTime = time.Now()
	newStep.ProgressTime = time.Unix(0, 0)

	// IMPORTANT: statesha has to be before state as that will be escaped
	newStep.StateSha, err = utils.StateSha(&newStep.State)

	if err != nil {
		rest.Error(w, "Error calculating Sha "+err.Error(), http.StatusInternalServerError)
		return
	}

	objectList, err := ProcessObjectsInState(newStep.Owner, newStep.State, a)
	if err != nil {
		rest.Error(w, "Error processing step objects in state:"+err.Error(), http.StatusInternalServerError)
		return
	}
	newStep.UsedObjects = objectList
	newStep.State = utils.BsonQuoteMap(&newStep.State)
	if newStep.Meta == nil {
		newStep.Meta = map[string]interface{}{}
	}
	newStep.Meta = utils.BsonQuoteMap(&newStep.Meta)

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = collSteps.InsertOne(
		ctx,
		newStep,
	)

	if err != nil {
		// XXX: figure how to be better on error cases here...
		rest.Error(w, "No access to resource or bad step rev1 "+err.Error(), http.StatusInternalServerError)
		return
	}
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updateResult, err := collTrails.UpdateOne(
		ctx,
		bson.M{
			"_id":     trail.Id,
			"garbage": bson.M{"$ne": true},
		},
		bson.M{"$set": bson.M{
			"last-touched": newStep.StepTime,
		}},
	)
	if updateResult.MatchedCount == 0 {
		rest.Error(w, "Trail not found", http.StatusBadRequest)
		return
	}
	if err != nil {
		// XXX: figure how to be better on error cases here...
		log.Printf("Error updating last-touched for trail in poststep; not failing because step was written: %s\n  => ERROR: %s\n ", trail.Id.Hex(), err.Error())
	}

	newStep.State = utils.BsonUnquoteMap(&newStep.State)
	newStep.Meta = utils.BsonUnquoteMap(&newStep.Meta)

	w.WriteJson(newStep)
}

//
// ## GET /trails/:id/steps
//   get steps of the the given trail.
//   For user accounts querying this will return the list of steps that are not
//   DONE or in error state.
//   For device accounts querying this will return the list of unconfirmed steps.
//   Devices confirm a step by posting a walk element matching the rev. This
//   conveyes that the devices knows about the step to go and will keep the
//   post updates to the walk elements as they go.
//
func (a *TrailsApp) handle_getsteps(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	steps := make([]Step, 0)

	trailId := r.PathParam("id")
	query := bson.M{}

	isPublic, err := a.isTrailPublic(trailId)

	if err != nil {
		rest.Error(w, "Error getting trail public", http.StatusInternalServerError)
		return
	}
	trailObjectID, err := primitive.ObjectIDFromHex(trailId)
	if err != nil {
		rest.Error(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	if isPublic {
		query = bson.M{
			"trail-id":        trailObjectID,
			"progress.status": "NEW",
			"garbage":         bson.M{"$ne": true},
		}
	} else if authType == "DEVICE" {
		query = bson.M{
			"trail-id":        trailObjectID,
			"device":          owner,
			"progress.status": "NEW",
			"garbage":         bson.M{"$ne": true},
		}
	} else if authType == "USER" {
		query = bson.M{
			"trail-id":        trailObjectID,
			"owner":           owner,
			"progress.status": bson.M{"$ne": "DONE"},
			"garbage":         bson.M{"$ne": true},
		}
	}

	// allow override of progress.status defaults
	progress_status := r.URL.Query().Get("progress.status")
	if progress_status != "" {
		m := map[string]interface{}{}
		err := json.Unmarshal([]byte(progress_status), &m)
		if err != nil {
			query["progress.status"] = progress_status
		} else {
			query["progress.status"] = m
		}
	}

	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	if authType == "DEVICE" {
		findOptions.SetLimit(1)
	}
	findOptions.SetSort(bson.M{"rev": 1}) //order by rev asc

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cur, err := coll.Find(ctx, query, findOptions)
	if err != nil {
		rest.Error(w, "Error on fetching steps:"+err.Error(), http.StatusForbidden)
		return
	}
	defer cur.Close(ctx)

	for cur.Next(ctx) {
		result := Step{}
		err := cur.Decode(&result)
		if err != nil {
			rest.Error(w, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
			return
		}
		result.Meta = utils.BsonUnquoteMap(&result.Meta)
		result.State = utils.BsonUnquoteMap(&result.State)
		steps = append(steps, result)
	}
	w.WriteJson(steps)
}

//
// ## GET /trails/:id/steps/:rev
//   get step
//
//   Both user and device accounts can read the steps they own or who they are the
//   device of. devices can PUT progress to the /progress pseudo subnode. Besides
//   that steps are read only for the matter of the API
//
func (a *TrailsApp) handle_getstep(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	trailId := r.PathParam("id")

	isPublic, err := a.isTrailPublic(trailId)

	if err != nil {
		rest.Error(w, "Error getting trail public", http.StatusInternalServerError)
	}

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	step := Step{}
	rev := r.PathParam("rev")

	query := bson.M{
		"_id":     trailId + "-" + rev,
		"garbage": bson.M{"$ne": true},
	}

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	if isPublic {
		err = coll.FindOne(ctx, query).Decode(&step)
	} else if authType == "DEVICE" {
		query["device"] = owner
		err = coll.FindOne(ctx, query).Decode(&step)
	} else if authType == "USER" {
		query["owner"] = owner
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailId + "-" + rev,
			"owner":   owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	} else {
		rest.Error(w, "No Access to step", http.StatusForbidden)
		return
	}

	if err != nil {
		rest.Error(w, "No access", http.StatusInternalServerError)
		return
	}

	step.Meta = utils.BsonUnquoteMap(&step.Meta)
	step.State = utils.BsonUnquoteMap(&step.State)

	w.WriteJson(step)
}

//
// ## GET /trails/:id/steps/:rev/meta
//   get step meta
//
//   just the raw data of a step without metainfo...
//
func (a *TrailsApp) handle_getstepmeta(w rest.ResponseWriter, r *rest.Request) {

	var err error

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	step := Step{}
	trailId := r.PathParam("id")
	rev := r.PathParam("rev")

	isPublic, err := a.isTrailPublic(trailId)

	if err != nil {
		rest.Error(w, "Error getting trail public", http.StatusInternalServerError)
		return
	}

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	if isPublic {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailId + "-" + rev,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	} else if authType == "DEVICE" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailId + "-" + rev,
			"device":  owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	} else if authType == "USER" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailId + "-" + rev,
			"owner":   owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	}

	if step.Meta == nil {
		step.Meta = map[string]interface{}{}
	}

	w.WriteJson(utils.BsonUnquoteMap(&step.Meta))
}

//
// ## GET /trails/:id/steps/:rev/state
//   get step state
//
//   just the raw data of a step without metainfo...
//
func (a *TrailsApp) handle_getstepstate(w rest.ResponseWriter, r *rest.Request) {

	var err error

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	step := Step{}

	trailId := r.PathParam("id")
	rev := r.PathParam("rev")

	isPublic, err := a.isTrailPublic(trailId)

	if err != nil {
		rest.Error(w, "Error getting trail public", http.StatusInternalServerError)
		return
	}

	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	if isPublic {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailId + "-" + rev,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	} else if authType == "DEVICE" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailId + "-" + rev,
			"device":  owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	} else if authType == "USER" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailId + "-" + rev,
			"owner":   owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	}

	w.WriteJson(utils.BsonUnquoteMap(&step.State))
}

func (a *TrailsApp) handle_getstepsobjects(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	step := Step{}

	trailId := r.PathParam("id")
	rev := r.PathParam("rev")

	isPublic, err := a.isTrailPublic(trailId)

	if err != nil {
		rest.Error(w, "Error getting trail public", http.StatusInternalServerError)
		return
	}
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)

	if isPublic {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailId + "-" + rev,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	} else if authType == "DEVICE" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailId + "-" + rev,
			"device":  owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	} else if authType == "USER" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailId + "-" + rev,
			"owner":   owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	}

	var objectsWithAccess []objects.ObjectWithAccess
	objectsWithAccess = make([]objects.ObjectWithAccess, 0)

	stateU := utils.BsonUnquoteMap(&step.State)

	for k, v := range stateU {
		_, ok := v.(string)

		if !ok {
			// we found a json element
			continue
		}

		if k == "#spec" {
			continue
		}

		collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

		if collection == nil {
			rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
			return
		}

		callingPrincipalStr, ok := owner.(string)
		if !ok {
			// XXX: find right error
			rest.Error(w, "Invalid Access", http.StatusForbidden)
			return
		}

		objID := v.(string)
		sha, err := utils.DecodeSha256HexString(objID)

		if err != nil {
			rest.Error(w, "Get Steps Object id must be a valid sha256", http.StatusBadRequest)
			return
		}

		storageId := objects.MakeStorageId(step.Owner, sha)

		var newObject objects.Object
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = collection.FindOne(ctx, bson.M{
			"_id":     storageId,
			"garbage": bson.M{"$ne": true},
		}).Decode(&newObject)

		if err != nil {
			rest.Error(w, "Not Accessible Resource Id: "+storageId+" ERR: "+err.Error(), http.StatusForbidden)
			return
		}

		if newObject.Owner != step.Owner {
			rest.Error(w, "Invalid Object Access", http.StatusForbidden)
			return
		}

		newObject.ObjectName = k

		issuerUrl := utils.GetApiEndpoint("/trails")
		objWithAccess := objects.MakeObjAccessible(issuerUrl, callingPrincipalStr, newObject, storageId)
		objectsWithAccess = append(objectsWithAccess, objWithAccess)
	}
	w.WriteJson(&objectsWithAccess)
}

func (a *TrailsApp) handle_poststepsobject(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	step := Step{}

	trailId := r.PathParam("id")
	rev := r.PathParam("rev")

	if authType != "DEVICE" && authType != "USER" {
		rest.Error(w, "Unknown AuthType", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := coll.FindOne(ctx, bson.M{
		"_id":     trailId + "-" + rev,
		"garbage": bson.M{"$ne": true},
	}).
		Decode(&step)
	if err != nil {
		rest.Error(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if authType == "DEVICE" && step.Device != owner {
		rest.Error(w, "No access for device", http.StatusForbidden)
		return
	} else if authType == "USER" && step.Owner != owner {
		rest.Error(w, "No access for user", http.StatusForbidden)
		return
	}

	newObject := objects.Object{}
	r.DecodeJsonPayload(&newObject)

	newObject.Owner = step.Owner

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	sha, err := utils.DecodeSha256HexString(newObject.Sha)

	if err != nil {
		rest.Error(w, "Post Steps Object id must be a valid sha256", http.StatusBadRequest)
		return
	}

	storageId := objects.MakeStorageId(newObject.Owner, sha)
	newObject.StorageId = storageId
	newObject.Id = newObject.Sha

	objects.SyncObjectSizes(&newObject)

	result, err := objects.CalcUsageAfterPost(newObject.Owner, a.mongoClient, newObject.Id, newObject.SizeInt)

	if err != nil {
		log.Println("Error to calc diskquota: " + err.Error())
		rest.Error(w, "Error posting object", http.StatusInternalServerError)
		return
	}

	quota, err := objects.GetDiskQuota(newObject.Owner)

	if err != nil {
		log.Println("Error get diskquota setting: " + err.Error())
		rest.Error(w, "Error to calc quota", http.StatusInternalServerError)
		return
	}

	if result.Total > quota {
		rest.Error(w, "Quota exceeded; delete some objects or request a quota bump from team@pantahub.com",
			http.StatusPreconditionFailed)
	}

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = collection.InsertOne(
		ctx,
		newObject,
	)

	if err != nil {
		filePath, err := utils.MakeLocalS3PathForName(storageId)

		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Header().Add("X-PH-Error", "Error Finding Path for Name"+err.Error())
			return
		}

		sd := storagedriver.FromEnv()
		if sd.Exists(filePath) {
			w.WriteHeader(http.StatusConflict)
			w.Header().Add("X-PH-Error", "Cannot insert existing object into database")
			goto conflict
		}

		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		updatedResult, err := collection.UpdateOne(
			ctx,
			bson.M{"_id": newObject.StorageId},
			bson.M{"$set": newObject},
		)
		if updatedResult.MatchedCount == 0 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Header().Add("X-PH-Error", "Error updating previously failed object in database ")
			return
		}
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Header().Add("X-PH-Error", "Error updating previously failed object in database "+err.Error())
			return
		}
		// we return anyway with the already available info about this object
	}
conflict:
	issuerUrl := utils.GetApiEndpoint("/trails")
	newObjectWithAccess := objects.MakeObjAccessible(issuerUrl, newObject.Owner, newObject, storageId)
	w.WriteJson(newObjectWithAccess)
}

func (a *TrailsApp) handle_putstepsobject(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	step := Step{}
	trailId := r.PathParam("id")
	rev := r.PathParam("rev")
	putId := r.PathParam("obj")

	if authType != "DEVICE" && authType != "USER" {
		rest.Error(w, "Unknown AuthType", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := coll.FindOne(ctx, bson.M{
		"_id":     trailId + "-" + rev,
		"garbage": bson.M{"$ne": true},
	}).Decode(&step)
	if err != nil {
		rest.Error(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if authType == "DEVICE" && step.Device != owner {
		rest.Error(w, "No access for device", http.StatusForbidden)
		return
	} else if authType == "USER" && step.Owner != owner {
		rest.Error(w, "No access for user", http.StatusForbidden)
		return
	}

	newObject := objects.Object{}
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	sha, err := utils.DecodeSha256HexString(putId)

	if err != nil {
		rest.Error(w, "Put Trails Steps Object id must be a valid sha256", http.StatusBadRequest)
		return
	}

	storageId := objects.MakeStorageId(step.Owner, sha)

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err = collection.FindOne(ctx, bson.M{
		"_id":     storageId,
		"garbage": bson.M{"$ne": true},
	}).Decode(&newObject)

	if err != nil {
		rest.Error(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if newObject.Owner != step.Owner {
		rest.Error(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	nId := newObject.Id
	nOwner := newObject.Owner
	nStorageId := newObject.StorageId
	r.DecodeJsonPayload(&newObject)

	if newObject.Id != nId {
		rest.Error(w, "Illegal Call Parameter Id", http.StatusConflict)
		return
	}
	if newObject.Owner != nOwner {
		rest.Error(w, "Illegal Call Parameter Owner", http.StatusConflict)
		return
	}
	if newObject.StorageId != nStorageId {
		rest.Error(w, "Illegal Call Parameter StorageId", http.StatusConflict)
		return
	}

	objects.SyncObjectSizes(&newObject)
	result, err := objects.CalcUsageAfterPut(newObject.Owner, a.mongoClient, newObject.Id, newObject.SizeInt)

	if err != nil {
		log.Println("Error to calc diskquota: " + err.Error())
		rest.Error(w, "Error posting object", http.StatusInternalServerError)
		return
	}

	quota, err := objects.GetDiskQuota(newObject.Owner)

	if err != nil {
		log.Println("Error get diskquota setting: " + err.Error())
		rest.Error(w, "Error to calc quota", http.StatusInternalServerError)
		return
	}

	if result.Total > quota {
		rest.Error(w, "Quota exceeded; delete some objects or request a quota bump from team@pantahub.com",
			http.StatusPreconditionFailed)
	}

	ctx, _ = context.WithTimeout(context.Background(), 5*time.Second)
	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	updateResult, err := collection.UpdateOne(
		ctx,
		bson.M{"_id": storageId},
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

	issuerUrl := utils.GetApiEndpoint("/trails")
	newObjectWithAccess := objects.MakeObjAccessible(issuerUrl, newObject.Owner, newObject, storageId)
	w.WriteJson(newObjectWithAccess)
}

func (a *TrailsApp) handle_getstepsobject(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	step := Step{}

	trailId := r.PathParam("id")
	rev := r.PathParam("rev")
	objIdParam := r.PathParam("obj")

	isPublic, err := a.isTrailPublic(trailId)
	if err != nil {
		rest.Error(w, "Error getting traitrailsIdl public", http.StatusInternalServerError)
		return
	}
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)

	if isPublic {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailId + "-" + rev,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	} else if authType == "DEVICE" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailId + "-" + rev,
			"device":  owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	} else if authType == "USER" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailId + "-" + rev,
			"owner":   owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	}
	if err != nil {
		rest.Error(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	stateU := utils.BsonUnquoteMap(&step.State)

	var objWithAccess *objects.ObjectWithAccess

	for k, v := range stateU {
		_, ok := v.(string)

		if !ok {
			// we found a json element
			continue
		}

		if k == "#spec" {
			continue
		}

		collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

		if collection == nil {
			rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
			return
		}

		callingPrincipalStr, ok := owner.(string)
		if !ok {
			// XXX: find right error
			rest.Error(w, "Invalid Access", http.StatusForbidden)
			return
		}

		objId := v.(string)

		if objIdParam != objId {
			continue
		}

		sha, err := utils.DecodeSha256HexString(objId)

		if err != nil {
			rest.Error(w, "Get Trails Steps Object id must be a valid sha256", http.StatusBadRequest)
			return
		}

		storageId := objects.MakeStorageId(step.Owner, sha)

		var newObject objects.Object
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = collection.FindOne(ctx, bson.M{
			"_id":     storageId,
			"garbage": bson.M{"$ne": true},
		}).
			Decode(&newObject)

		if err != nil {
			rest.Error(w, "Not Accessible Resource Id: "+storageId+" ERR: "+err.Error(), http.StatusForbidden)
			return
		}

		if newObject.Owner != step.Owner {
			rest.Error(w, "Invalid Object Access ("+newObject.Owner+":"+step.Owner+")", http.StatusForbidden)
			return
		}

		newObject.ObjectName = k

		issuerUrl := utils.GetApiEndpoint("/trails")
		tmp := objects.MakeObjAccessible(issuerUrl, callingPrincipalStr, newObject, storageId)
		objWithAccess = &tmp
		break
	}

	if objWithAccess != nil {
		w.WriteJson(&objWithAccess)
	} else {
		rest.Error(w, "Invalid Object", http.StatusForbidden)
	}
}

func (a *TrailsApp) handle_getstepsobjectfile(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	step := Step{}

	trailId := r.PathParam("id")
	rev := r.PathParam("rev")
	objIdParam := r.PathParam("obj")

	isPublic, err := a.isTrailPublic(trailId)

	if err != nil {
		rest.Error(w, "Error getting trail public", http.StatusInternalServerError)
		return
	}
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	if isPublic {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailId + "-" + rev,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	} else if authType == "DEVICE" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailId + "-" + rev,
			"device":  owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	} else if authType == "USER" {
		err = coll.FindOne(ctx, bson.M{
			"_id":     trailId + "-" + rev,
			"owner":   owner,
			"garbage": bson.M{"$ne": true},
		}).Decode(&step)
	}
	if err != nil {
		rest.Error(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	stateU := utils.BsonUnquoteMap(&step.State)

	var objWithAccess *objects.ObjectWithAccess

	for k, v := range stateU {
		_, ok := v.(string)

		if !ok {
			// we found a json element
			continue
		}

		if k == "#spec" {
			continue
		}

		collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")

		if collection == nil {
			rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
			return
		}

		callingPrincipalStr, ok := owner.(string)
		if !ok {
			// XXX: find right error
			rest.Error(w, "Invalid Access", http.StatusForbidden)
			return
		}

		objId := v.(string)

		if objIdParam != objId {
			continue
		}

		sha, err := utils.DecodeSha256HexString(objId)

		if err != nil {
			rest.Error(w, "Get Trails Steps Object File by ID must be a valid sha256", http.StatusBadRequest)
			return
		}

		storageId := objects.MakeStorageId(step.Owner, sha)

		var newObject objects.Object
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = collection.FindOne(ctx, bson.M{
			"_id":     storageId,
			"garbage": bson.M{"$ne": true},
		}).Decode(&newObject)

		if err != nil {
			rest.Error(w, "Not Accessible Resource Id: "+storageId+" ERR: "+err.Error(), http.StatusForbidden)
			return
		}

		if newObject.Owner != step.Owner {
			rest.Error(w, "Invalid Object Access", http.StatusForbidden)
			return
		}

		newObject.ObjectName = k

		issuerUrl := utils.GetApiEndpoint("/trails")
		tmp := objects.MakeObjAccessible(issuerUrl, callingPrincipalStr, newObject, storageId)
		objWithAccess = &tmp
		break
	}

	if objWithAccess == nil {
		rest.Error(w, "Invalid Object", http.StatusForbidden)
		return
	}

	url := objWithAccess.SignedGetUrl
	w.Header().Add("Location", url)
	w.WriteHeader(http.StatusFound)
}

//
// ## PUT /trails/:id/steps/:rev/state
//   put step state (only if not yet consumed)
//
//   just the raw data of a step without metainfo like pvr put ...
//
func (a *TrailsApp) handle_putstepstate(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")
	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	step := Step{}
	trailId := r.PathParam("id")
	rev := r.PathParam("rev")

	if authType != "USER" {
		rest.Error(w, "Need to be logged in as USER to put step state", http.StatusForbidden)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := coll.FindOne(ctx, bson.M{
		"_id":             trailId + "-" + rev,
		"progress.status": "NEW",
		"garbage":         bson.M{"$ne": true},
	}).Decode(&step)

	if err != nil {
		rest.Error(w, "Error with accessing data: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if step.Owner != owner {
		rest.Error(w, "No write access to step state", http.StatusForbidden)
	}

	stateMap := map[string]interface{}{}
	err = r.DecodeJsonPayload(&stateMap)
	if err != nil {
		rest.Error(w, "Error with request: "+err.Error(), http.StatusBadRequest)
		return
	}

	step.StateSha, err = utils.StateSha(&stateMap)

	step.StepTime = time.Now()
	step.ProgressTime = time.Unix(0, 0)
	step.Id = trailId + "-" + rev

	objectList, err := ProcessObjectsInState(step.Owner, stateMap, a)
	if err != nil {
		rest.Error(w, "Error processing step objects in state:"+err.Error(), http.StatusInternalServerError)
		return
	}
	step.UsedObjects = objectList
	step.State = utils.BsonQuoteMap(&stateMap)

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updateResult, err := coll.UpdateOne(
		ctx,
		bson.M{
			"_id":             trailId + "-" + rev,
			"owner":           owner,
			"progress.status": "NEW",
			"garbage":         bson.M{"$ne": true},
		},
		bson.M{"$set": step},
	)
	if updateResult.MatchedCount == 0 {
		rest.Error(w, "Error updating step state: not found", http.StatusBadRequest)
		return
	}

	if err != nil {
		rest.Error(w, "Error updating step state: "+err.Error(), http.StatusInternalServerError)
		return
	}

	step.State = utils.BsonUnquoteMap(&step.State)
	w.WriteJson(step.State)
}

//
// ## PUT /trails/:id/steps/:rev/meta
//   put step meta
//
//   just the raw data of a step without metainfo like pvr put ...
//
func (a *TrailsApp) handle_putstepmeta(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")
	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	step := Step{}
	trailId := r.PathParam("id")
	rev := r.PathParam("rev")

	if authType != "USER" {
		rest.Error(w, "Need to be logged in as USER to put step meta", http.StatusForbidden)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := coll.FindOne(ctx, bson.M{
		"_id":     trailId + "-" + rev,
		"garbage": bson.M{"$ne": true},
	}).Decode(&step)

	if err != nil {
		rest.Error(w, "Error with accessing data: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if step.Owner != owner {
		rest.Error(w, "No write access to step meta", http.StatusForbidden)
	}

	metaMap := map[string]interface{}{}
	err = r.DecodeJsonPayload(&metaMap)
	if err != nil {
		rest.Error(w, "Error with request: "+err.Error(), http.StatusBadRequest)
		return
	}

	step.Meta = utils.BsonQuoteMap(&metaMap)

	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updateResult, err := coll.UpdateOne(
		ctx,
		bson.M{
			"_id":     trailId + "-" + rev,
			"owner":   owner,
			"garbage": bson.M{"$ne": true},
		},
		bson.M{"$set": step},
	)
	if updateResult.MatchedCount == 0 {
		rest.Error(w, "Error updating step meta: not found", http.StatusBadRequest)
		return
	}

	if err != nil {
		rest.Error(w, "Error updating step meta: "+err.Error(), http.StatusInternalServerError)
		return
	}

	step.Meta = utils.BsonUnquoteMap(&step.Meta)
	w.WriteJson(step.Meta)
}

//
// ## PUT /trails/:id/steps/:rev/progress
//   Post Step Progress information for a step.
//
//   Only device accounts can put status info. they are expected to provide at
//   status field.
//   all input paramaters besides the device-progress one are ignored.
//
//
func (a *TrailsApp) handle_putstepprogress(w rest.ResponseWriter, r *rest.Request) {

	stepProgress := StepProgress{}
	r.DecodeJsonPayload(&stepProgress)
	trailId := r.PathParam("id")
	stepId := trailId + "-" + r.PathParam("rev")

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	coll := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	collTrails := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_trails")

	if collTrails == nil {
		rest.Error(w, "Error with Database connectivity - trails", http.StatusInternalServerError)
		return
	}

	if authType != "DEVICE" {
		rest.Error(w, "Only devices can update step status", http.StatusForbidden)
		return
	}

	progressTime := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	updateResult, err := coll.UpdateOne(
		ctx,
		bson.M{
			"_id":     stepId,
			"device":  owner,
			"garbage": bson.M{"$ne": true},
		},
		bson.M{"$set": bson.M{
			"progress":      stepProgress,
			"progress-time": progressTime,
		}},
	)
	if updateResult.MatchedCount == 0 {
		rest.Error(w, "Error updating trail: not found", http.StatusBadRequest)
		return
	}

	if err != nil {
		rest.Error(w, "Cannot update step progress "+err.Error(), http.StatusForbidden)
		return
	}
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	trailObjectID, err := primitive.ObjectIDFromHex(trailId)
	if err != nil {
		rest.Error(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	updateResult, err = collTrails.UpdateOne(
		ctx,
		bson.M{
			"_id":     trailObjectID,
			"garbage": bson.M{"$ne": true},
		},
		bson.M{"$set": bson.M{"last-touched": progressTime}},
	)
	if updateResult.MatchedCount == 0 {
		rest.Error(w, "Error updating trail: not found", http.StatusBadRequest)
		return
	}

	if err != nil {
		// XXX: figure how to be better on error cases here...
		log.Printf("Error updating last-touched for trail in poststepprogress; not failing because step was written: %s\n", trailId)
	}

	w.WriteJson(stepProgress)
}

//
// ## GET /trails/:id/summary
//   get steps of the the given trail.
//   For user accounts querying this will return the list of steps that are not
//   DONE or in error state.
//   For device accounts querying this will return the list of unconfirmed steps.
//   Devices confirm a step by posting a walk element matching the rev. This
//   conveyes that the devices knows about the step to go and will keep the
//   post updates to the walk elements as they go.
//
func (a *TrailsApp) handle_gettrailstepsummary(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	summaryCol := a.mongoClient.Database("pantabase_devicesummary").Collection("device_summary_short_new_v2")

	if summaryCol == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if authType != "USER" {
		rest.Error(w, "Need to be logged in as USER to get trail summary", http.StatusForbidden)
		return
	}

	trailId := r.PathParam("id")

	if trailId == "" {
		rest.Error(w, "need to specify a device id", http.StatusForbidden)
		return
	}

	query := bson.M{
		"deviceid": trailId,
		"garbage":  bson.M{"$ne": true},
		"$or": []bson.M{
			{"owner": owner},
			{"public": true},
		},
	}

	summary := TrailSummary{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := summaryCol.FindOne(ctx, query).Decode(&summary)

	if err != nil {
		rest.Error(w, "error finding new trailId", http.StatusForbidden)
		return
	}

	if owner != summary.Owner {
		summary.FleetGroup = ""
		summary.FleetLocation = ""
		summary.FleetModel = ""
		summary.FleetRev = ""
		summary.RealIP = ""
	}
	w.WriteJson(summary)
}

//
// ## GET /trails/summary
//   get summary of all trails by the calling owner.
func (a *TrailsApp) handle_gettrailsummary(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	summaryCol := a.mongoClient.Database("pantabase_devicesummary").Collection("device_summary_short_new_v2")

	if summaryCol == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"]

	if authType != "USER" {
		rest.Error(w, "Need to be logged in as USER to get trail summary", http.StatusForbidden)
		return
	}

	sortParam := r.FormValue("sort")

	if sortParam == "" {
		sortParam = "-timestamp"
	}

	m := bson.M{}
	filterParam := r.FormValue("filter")
	if filterParam != "" {
		err := json.Unmarshal([]byte(filterParam), &m)
		if err != nil {
			rest.Error(w, "Illegal Filter "+err.Error(), http.StatusBadRequest)
			return
		}
	}

	// always filter by owner...
	m["owner"] = owner
	m["garbage"] = bson.M{"$ne": true}

	summaries := make([]TrailSummary, 0)

	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	if sortParam[0:0] == "-" {
		sortParam = sortParam[1:] //removing "-"
		findOptions.SetSort(bson.M{sortParam: -1})
	} else {
		findOptions.SetSort(bson.M{sortParam: 1})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cur, err := summaryCol.Find(ctx, m, findOptions)
	if err != nil {
		rest.Error(w, "Error on fetching summaries:"+err.Error(), http.StatusForbidden)
		return
	}
	defer cur.Close(ctx)
	for cur.Next(ctx) {
		result := TrailSummary{}
		err := cur.Decode(&result)
		if err != nil {
			rest.Error(w, "Cursor Decode Error:"+err.Error(), http.StatusForbidden)
			return
		}
		summaries = append(summaries, result)
	}

	w.WriteJson(summaries)
}

// ProcessObjectsInState :
/*
1.Get Object List from the State field
2.UnMark All Objects As Garbages if they are marked as garbage
*/
func ProcessObjectsInState(
	owner string,
	state map[string]interface{},
	a *TrailsApp,
) (
	objects []string,
	err error,
) {
	objectList, err := GetStateObjects(owner, state, a)
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
	a *TrailsApp,
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
		return objectList, errors.New("state_object: Invalid state:#spec is missing")
	}

	specValue, ok := spec.(string)
	if !ok {
		return objectList, errors.New("state_object: Invalid state:Value of #spec should be string")
	}

	if specValue != "pantavisor-multi-platform@1" &&
		specValue != "pantavisor-service-system@1" {
		return objectList, errors.New("state_object: Invalid state:Value of #spec should not be " + specValue)
	}

	for key, v := range state {
		if strings.HasSuffix(key, ".json") ||
			key == "#spec" {
			continue
		}
		sha, found := v.(string)
		if !found {
			return objectList, errors.New("state_object:Object is not a string[sha:" + sha + "]")
		}
		shaBytes, err := utils.DecodeSha256HexString(sha)
		if err != nil {
			return objectList, errors.New("state_object: Object sha that could not be decoded from hex:" + err.Error() + " [sha:" + sha + "]")
		}
		// lets use proper storage shas to reflect that fact that each
		// owner has its own copy of the object instance on DB side
		storageSha := objects.MakeStorageId(owner, shaBytes)
		result, _ := IsObjectValid(storageSha, a)
		if !result {
			return objectList, errors.New("state_object: Object sha is not found in the db[storage-id(_id):" + storageSha + "]")
		}
		if _, ok := objMap[storageSha]; !ok {
			objectList = append(objectList, storageSha)
		}
	}
	return objectList, nil
}

// RestoreObjects : Takes the list of objects and unmarks them garbage.
func RestoreObjects(
	objectList []string,
	a *TrailsApp,
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

// IsObjectValid : to check if an object is valid or not
func IsObjectValid(ObjectID string, a *TrailsApp) (
	result bool,
	errs error,
) {
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_objects")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	objectCount, err := collection.CountDocuments(ctx,
		bson.M{
			"_id": ObjectID,
		},
	)
	if err != nil {
		return false, errors.New("Error Finding Object:" + err.Error())
	}
	return (objectCount == 1), nil
}

// IsObjectGarbage : to check if an object is garbage or not
func IsObjectGarbage(ObjectID string, a *TrailsApp) (
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
func UnMarkObjectAsGarbage(ObjectID string, a *TrailsApp) error {
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

// XXX:
//   finish getsteps
//   post walk
//   get walks
//   search attributes for advanced steps/walk searching inside trail
//
func New(jwtMiddleware *jwt.JWTMiddleware, mongoClient *mongo.Client) *TrailsApp {

	app := new(TrailsApp)
	app.jwt_middleware = jwtMiddleware
	app.mongoClient = mongoClient

	// Indexing for the owner,garbage fields in pantahub_trails
	collection := app.mongoClient.Database(utils.MongoDb).Collection("pantahub_trails")

	CreateIndexesOptions := options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions := options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index := mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "owner", Value: bsonx.Int32(1)},
			{Key: "garbage", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	_, err := collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_trails: " + err.Error())
		return nil
	}

	// Indexing for the device,garbage fields in pantahub_trails
	collection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_trails")

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "device", Value: bsonx.Int32(1)},
			{Key: "garbage", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_trails: " + err.Error())
		return nil
	}

	// Indexing for the owner,garbage fields in pantahub_steps
	collection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "owner", Value: bsonx.Int32(1)},
			{Key: "garbage", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_steps: " + err.Error())
		return nil
	}

	// Indexing for the device,garbage fields in pantahub_steps
	collection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_steps")

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(false)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "device", Value: bsonx.Int32(1)},
			{Key: "garbage", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_steps: " + err.Error())
		return nil
	}

	app.Api = rest.NewApi()

	// we dont use default stack because we dont want content type enforcement
	app.Api.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/trails:", log.Lshortfile)})
	app.Api.Use(&utils.AccessLogFluentMiddleware{Prefix: "trails"})
	app.Api.Use(&rest.StatusMiddleware{})
	app.Api.Use(&rest.TimerMiddleware{})
	app.Api.Use(&metrics.MetricsMiddleware{})

	app.Api.Use(rest.DefaultCommonStack...)
	app.Api.Use(&rest.CorsMiddleware{
		RejectNonCorsRequests: false,
		OriginValidator: func(origin string, request *rest.Request) bool {
			return true
		},
		AllowedMethods: []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders: []string{
			"Accept", "Content-Type", "X-Custom-Header", "Origin", "Authorization"},
		AccessControlAllowCredentials: true,
		AccessControlMaxAge:           3600,
	})
	app.Api.Use(&utils.URLCleanMiddleware{})

	app.Api.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			return true
		},
		IfTrue: app.jwt_middleware,
	})
	app.Api.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			return true
		},
		IfTrue: &utils.AuthMiddleware{},
	})

	// /auth_status endpoints
	// XXX: this is all needs to be done so that paths that do not trail with /
	//      get a MOVED PERMANTENTLY error with the redir path with / like the main
	//      API routers (bad rest.MakeRouter I suspect)

	readTrailsScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Trails,
		utils.Scopes.ReadTrails,
	}
	writeTrailsScopes := []utils.Scope{
		utils.Scopes.API,
		utils.Scopes.Trails,
		utils.Scopes.WriteTrails,
	}
	api_router, _ := rest.MakeRouter(
		rest.Get("/auth_status", utils.ScopeFilter(readTrailsScopes, handle_auth)),
		rest.Get("/", utils.ScopeFilter(readTrailsScopes, app.handle_gettrails)),
		rest.Post("/", utils.ScopeFilter(writeTrailsScopes, app.handle_posttrail)),
		rest.Get("/summary", utils.ScopeFilter(readTrailsScopes, app.handle_gettrailsummary)),
		rest.Get("/:id", utils.ScopeFilter(readTrailsScopes, app.handle_gettrail)),
		rest.Get("/:id/.pvrremote", utils.ScopeFilter(readTrailsScopes, app.handle_gettrailpvrinfo)),
		rest.Post("/:id/steps", utils.ScopeFilter(writeTrailsScopes, app.handle_poststep)),
		rest.Get("/:id/steps", utils.ScopeFilter(readTrailsScopes, app.handle_getsteps)),
		rest.Get("/:id/steps/:rev", utils.ScopeFilter(readTrailsScopes, app.handle_getstep)),
		rest.Get("/:id/steps/:rev/.pvrremote", utils.ScopeFilter(readTrailsScopes, app.handle_getsteppvrinfo)),
		rest.Get("/:id/steps/:rev/meta", utils.ScopeFilter(readTrailsScopes, app.handle_getstepmeta)),
		rest.Get("/:id/steps/:rev/state", utils.ScopeFilter(readTrailsScopes, app.handle_getstepstate)),
		rest.Get("/:id/steps/:rev/objects", utils.ScopeFilter(readTrailsScopes, app.handle_getstepsobjects)),
		rest.Post("/:id/steps/:rev/objects", utils.ScopeFilter(writeTrailsScopes, app.handle_poststepsobject)),
		rest.Get("/:id/steps/:rev/objects/:obj", utils.ScopeFilter(readTrailsScopes, app.handle_getstepsobject)),
		rest.Get("/:id/steps/:rev/objects/:obj/blob", utils.ScopeFilter(readTrailsScopes, app.handle_getstepsobjectfile)),
		rest.Put("/:id/steps/:rev/meta", utils.ScopeFilter(writeTrailsScopes, app.handle_putstepmeta)),
		rest.Put("/:id/steps/:rev/state", utils.ScopeFilter(writeTrailsScopes, app.handle_putstepstate)),
		rest.Put("/:id/steps/:rev/progress", utils.ScopeFilter(writeTrailsScopes, app.handle_putstepprogress)),
		rest.Get("/:id/summary", utils.ScopeFilter(readTrailsScopes, app.handle_gettrailstepsummary)),
	)
	app.Api.SetApp(api_router)

	return app
}
