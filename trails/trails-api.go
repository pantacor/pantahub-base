//
// Copyright 2017  Pantacor Ltd.
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
// highly asynchronous configuration management as found in edge compute device
// world.
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
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pvr/api"

	"github.com/StephanDollberg/go-json-rest-middleware-jwt"
	"github.com/ant0ine/go-json-rest/rest"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type TrailsApp struct {
	jwt_middleware *jwt.JWTMiddleware
	Api            *rest.Api
	mgoSession     *mgo.Session
}

type Trail struct {
	Id     bson.ObjectId `json:"id" bson:"_id"`
	Owner  string        `json:"owner"`
	Device string        `json:"device"`
	//  Admins   []string `json:"admins"`   // XXX: maybe this is best way to do delegating device access....
	LastInSync   time.Time              `json:"last-insync" bson:"last-insync"`
	LastTouched  time.Time              `json:"last-touched" bson:"last-touched"`
	FactoryState map[string]interface{} `json:"factory-state" bson:"factory-state"`
}

// step wanted can be added by the device owner or delegate.
// steps that were not reported can be deleted still. other steps
// cannot be deleted until the device gets deleted as well.
type Step struct {
	Id           string                 `json:"id" bson:"_id"` // XXX: make type
	Owner        string                 `json:"owner"`
	Device       string                 `json:"device"`
	Committer    string                 `json:"committer"`
	TrailId      bson.ObjectId          `json:"trail-id" bson:"trail-id"` //parent id
	Rev          int                    `json:"rev"`
	CommitMsg    string                 `json:"commit-msg" bson:"commit-msg"`
	State        map[string]interface{} `json:"state"` // json blurb
	StepProgress StepProgress           `json:"progress" bson:"progress"`
	StepTime     time.Time              `json:"step-time" bson:"step-time"`
	ProgressTime time.Time              `json:"progress-time" bson:"progress-time"`
}

type StepProgress struct {
	Progress  int    `json:"progress"`   // progress number. steps or 1-100
	StatusMsg string `json:"status-msg"` // message of progress status
	Status    string `json:"status"`     // status code
	Log       string `json:"log"`        // log if available
}

type TrailSummary struct {
	DeviceId         bson.ObjectId `json:"deviceid"`
	Device           string        `json:"device"`
	DeviceNick       string        `json:"device-nick"`
	Rev              int           `json:"revision"`
	ProgressRev      int           `json:"progress-revision"`
	Progress         int           `json:"progress"`   // progress number. steps or 1-100
	StatusMsg        string        `json:"status-msg"` // message of progress status
	Status           string        `json:"status"`     // status code
	StepTime         time.Time     `json:"step-time" bson:"step-time"`
	ProgressTime     time.Time     `json:"progress-time" bson:"progress-time"`
	TrailTouchedTime time.Time     `json:"trail-touched-time" bson:"trail-touched-time"`
}

func handle_auth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

// XXX: no product without fixing this to only parse ids that belong to this
// service instance
func prnGetId(prn string) string {
	idx := strings.Index(prn, "/")
	return prn[idx+1 : len(prn)]
}

// ## POST /trails/
//   usually done by device on first log in. This
//   initiates the trail by using the reported state as stepwanted 0 and setting
//   the step 0 to be the POSTED JSON. Either device accounts or user accounts can
//   do this for devices owned, but there can always only be ONE trail per device.
func (a *TrailsApp) handle_posttrail(w rest.ResponseWriter, r *rest.Request) {

	initialState := map[string]interface{}{}

	r.DecodeJsonPayload(&initialState)

	device, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	if authType != "DEVICE" {
		// XXX: find right error
		rest.Error(w, "You need to be logged in as a DEVICE to post new trails", http.StatusForbidden)
		return
	}

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["owner"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "Device needs an owner", http.StatusForbidden)
		return
	}

	// do we need tip/tail here? or is that always read-only?
	newTrail := Trail{}
	newTrail.Id = bson.ObjectIdHex(prnGetId(device.(string)))
	newTrail.Owner = owner.(string)
	newTrail.Device = device.(string)
	newTrail.LastInSync = time.Time{}
	newTrail.FactoryState = utils.BsonQuoteMap(&initialState)

	newStep := Step{}
	newStep.Id = newTrail.Id.Hex() + "-0"
	newStep.TrailId = newTrail.Id
	newStep.Rev = 0
	newStep.State = utils.BsonQuoteMap(&initialState)
	newStep.Owner = newTrail.Owner
	newStep.Device = newTrail.Device
	newStep.CommitMsg = "Factory State (rev 0)"
	newStep.StepTime = time.Now() // XXX this should be factory time not now
	newStep.ProgressTime = time.Now()
	newStep.StepProgress.Status = "DONE"

	collection := a.mgoSession.DB("").C("pantahub_trails")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	// XXX: prototype: for production we need to prevent posting twice!!
	err := collection.Insert(newTrail)

	if err != nil {
		rest.Error(w, "Error inserting trail into database "+err.Error(), http.StatusInternalServerError)
		return
	}

	collection = a.mgoSession.DB("").C("pantahub_steps")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	err = collection.Insert(newStep)

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

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	coll := a.mgoSession.DB("").C("pantahub_trails")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	trails := make([]Trail, 0)

	if authType == "DEVICE" {
		coll.Find(bson.M{"device": owner}).All(&trails)
		if len(trails) > 1 {
			fmt.Println("WARNING: more than one trail in db for device - bad DB: " + owner.(string))
			trails = trails[0:1]
		}
	} else if authType == "USER" {
		coll.Find(bson.M{"owner": owner}).All(&trails)
	}

	for k, v := range trails {
		v.FactoryState = utils.BsonUnquoteMap(&v.FactoryState)
		trails[k] = v
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

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	coll := a.mgoSession.DB("").C("pantahub_trails")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	getId := r.PathParam("id")
	trail := Trail{}

	var err error

	if authType == "DEVICE" {
		err = coll.Find(bson.M{"_id": getId, "device": owner}).One(&trail)
	} else if authType == "USER" {
		err = coll.Find(bson.M{"_id": getId, "owner": owner}).One(&trail)
	}

	if err != nil {
		rest.Error(w, "No access to resource: "+err.Error(), http.StatusInternalServerError)
		return
	}

	trail.FactoryState = utils.BsonUnquoteMap(&trail.FactoryState)
	w.WriteJson(trail)
}

func (a *TrailsApp) handle_gettrailpvrinfo(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	coll := a.mgoSession.DB("").C("pantahub_steps")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	getId := r.PathParam("id")
	step := Step{}

	var err error
	//	get last step
	if authType == "DEVICE" {
		err = coll.Find(bson.M{"device": owner, "trail-id": bson.ObjectIdHex(getId)}).Sort("-rev").One(&step)
	} else if authType == "USER" {
		err = coll.Find(bson.M{"owner": owner, "trail-id": bson.ObjectIdHex(getId)}).Sort("-rev").One(&step)
	}

	if err != nil {
		rest.Error(w, "No access to resource: "+err.Error(), http.StatusInternalServerError)
		return
	}

	oe := utils.GetApiEndpoint("/objects")
	jsonGet := utils.GetApiEndpoint("/trails/" + getId + "/steps/" + strconv.Itoa(step.Rev) + "/state")
	postUrl := utils.GetApiEndpoint("/trails/" + getId + "/steps")
	postFields := []string{"commit-msg"}
	postFieldsOpt := []string{"rev"}

	remoteInfo := pvrapi.PvrRemote{
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

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	coll := a.mgoSession.DB("").C("pantahub_steps")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	getId := r.PathParam("id")
	revId := r.PathParam("rev")
	stepId := getId + "-" + revId
	step := Step{}

	var err error
	//	get step and check right to access
	if authType == "DEVICE" {
		err = coll.Find(bson.M{"device": owner, "_id": stepId}).One(&step)
	} else if authType == "USER" {
		err = coll.Find(bson.M{"owner": owner, "_id": stepId}).One(&step)
	}

	if err != nil {
		rest.Error(w, "No access to resource: "+err.Error(), http.StatusInternalServerError)
		return
	}

	oe := utils.GetApiEndpoint("/objects")

	jsonUrl := utils.GetApiEndpoint("/trails/" + getId + "/steps/" +
		revId + "/state")

	postUrl := utils.GetApiEndpoint("/trails/" + getId + "/steps")
	postFields := []string{"msg"}
	postFieldsOpt := []string{}

	remoteInfo := pvrapi.PvrRemote{
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

func (a *TrailsApp) get_latest_steprev(trailId bson.ObjectId) (int, error) {
	collSteps := a.mgoSession.DB("").C("pantahub_steps")

	if collSteps == nil {
		return -1, errors.New("bad database connetivity")
	}

	step := &Step{}

	err := collSteps.Find(bson.M{"trail-id": trailId}).Sort("-rev").Limit(1).One(step)

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

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	collTrails := a.mgoSession.DB("").C("pantahub_trails")

	if collTrails == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	trailId := r.PathParam("id")
	trail := Trail{}

	var err error

	if authType == "USER" {
		err = collTrails.Find(bson.M{"_id": bson.ObjectIdHex(trailId), "owner": owner}).One(&trail)
	} else {
		rest.Error(w, "Need to be logged in as USER to post trail steps", http.StatusForbidden)
		return
	}

	if err != nil {
		rest.Error(w, "No resource access possible", http.StatusInternalServerError)
		return
	}

	collSteps := a.mgoSession.DB("").C("pantahub_steps")

	if collSteps == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	newStep := Step{}
	previousStep := Step{}
	r.DecodeJsonPayload(&newStep)

	if newStep.Rev == -1 {
		newStep.Rev, err = a.get_latest_steprev(bson.ObjectIdHex(trailId))
		newStep.Rev += 1
	}

	if err != nil {
		rest.Error(w, "Error auto appending step 1 "+err.Error(), http.StatusInternalServerError)
		return
	}

	stepId := trailId + "-" + strconv.Itoa(newStep.Rev-1)

	err = collSteps.Find(bson.M{"_id": stepId}).One(&previousStep)

	if err != nil {
		// XXX: figure how to be better on error cases here...
		rest.Error(w, "No access to resource or bad step rev", http.StatusInternalServerError)
		return
	}

	// XXX: introduce step diffs here and store them precalced

	newStep.Id = trail.Id.Hex() + "-" + strconv.Itoa(newStep.Rev)
	fmt.Printf("newStep.Id: %s\n", newStep.Id)

	newStep.Owner = trail.Owner
	newStep.Device = trail.Device
	newStep.StepProgress = StepProgress{
		Status: "NEW",
	}
	newStep.TrailId = trail.Id
	newStep.StepTime = time.Now()
	newStep.ProgressTime = time.Unix(0, 0)
	newStep.State = utils.BsonQuoteMap(&newStep.State)

	err = collSteps.Insert(newStep)

	if err != nil {
		// XXX: figure how to be better on error cases here...
		rest.Error(w, "No access to resource or bad step rev1 "+err.Error(), http.StatusInternalServerError)
		return
	}

	err = collTrails.Update(bson.M{"_id": trail.Id, "device": trail.Owner}, bson.M{"$set": bson.M{"last-touched": newStep.StepTime}})

	if err != nil {
		// XXX: figure how to be better on error cases here...
		fmt.Printf("Error updating last-touched for trail in poststep; not failing because step was written: %s\n", trail.Id.Hex())
	}

	newStep.State = utils.BsonUnquoteMap(&newStep.State)

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

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	coll := a.mgoSession.DB("").C("pantahub_steps")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	steps := make([]Step, 0)

	trailId := r.PathParam("id")
	query := bson.M{}
	if authType == "DEVICE" {
		query = bson.M{"trail-id": bson.ObjectIdHex(trailId), "device": owner, "progress.status": "NEW"}
	} else if authType == "USER" {
		query = bson.M{"trail-id": bson.ObjectIdHex(trailId), "owner": owner, "progress.status": bson.M{"$ne": "DONE"}}
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

	q := coll.Find(query).Sort("rev")

	var err error
	if authType == "DEVICE" {
		err = q.Limit(1).All(&steps)
	} else {
		err = q.All(&steps)
	}

	if err != nil {
		rest.Error(w, "Error getting trails step steps", http.StatusInternalServerError)
		return
	}

	for k, v := range steps {
		v.State = utils.BsonUnquoteMap(&v.State)
		steps[k] = v
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

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	coll := a.mgoSession.DB("").C("pantahub_steps")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	step := Step{}

	trailId := r.PathParam("id")
	rev := r.PathParam("rev")

	if authType == "DEVICE" {
		coll.Find(bson.M{"_id": trailId + "-" + rev, "device": owner}).One(&step)
	} else if authType == "USER" {
		coll.Find(bson.M{"_id": trailId + "-" + rev, "owner": owner}).One(&step)
	}

	step.State = utils.BsonUnquoteMap(&step.State)
	w.WriteJson(step)
}

//
// ## GET /trails/:id/steps/:rev/state
//   get step state
//
//   just the raw data of a step without metainfo...
//
func (a *TrailsApp) handle_getstepstate(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	coll := a.mgoSession.DB("").C("pantahub_steps")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	step := Step{}

	trailId := r.PathParam("id")
	rev := r.PathParam("rev")

	if authType == "DEVICE" {
		coll.Find(bson.M{"_id": trailId + "-" + rev, "device": owner}).One(&step)
	} else if authType == "USER" {
		coll.Find(bson.M{"_id": trailId + "-" + rev, "owner": owner}).One(&step)
	}

	w.WriteJson(utils.BsonUnquoteMap(&step.State))
}

//
// ## PUT /trails/:id/steps/:rev/state
//   put step state (only if not yet consumed)
//
//   just the raw data of a step without metainfo like pvr put ...
//
func (a *TrailsApp) handle_putstepstate(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	coll := a.mgoSession.DB("").C("pantahub_steps")
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

	err := coll.Find(bson.M{"_id": trailId + "-" + rev, "owner": owner, "progress.status": "NEW"}).One(&step)

	if err != nil {
		rest.Error(w, "Error with accessing data: "+err.Error(), http.StatusInternalServerError)
		return
	}

	stateMap := map[string]interface{}{}
	err = r.DecodeJsonPayload(&stateMap)
	if err != nil {
		rest.Error(w, "Error with request: "+err.Error(), http.StatusBadRequest)
		return
	}

	step.State = utils.BsonQuoteMap(&stateMap)
	step.StepTime = time.Now()
	step.ProgressTime = time.Unix(0, 0)

	step.Id = trailId + "-" + rev

	err = coll.Update(bson.M{"_id": trailId + "-" + rev, "owner": owner, "progress.status": "NEW"}, step)

	if err != nil {
		rest.Error(w, "Error updating step state: "+err.Error(), http.StatusInternalServerError)
		return
	}

	step.State = utils.BsonUnquoteMap(&step.State)
	w.WriteJson(step.State)
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

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	coll := a.mgoSession.DB("").C("pantahub_steps")

	if coll == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	collTrails := a.mgoSession.DB("").C("pantahub_trails")

	if collTrails == nil {
		rest.Error(w, "Error with Database connectivity - trails", http.StatusInternalServerError)
		return
	}

	if authType != "DEVICE" {
		rest.Error(w, "Only devices can update step status", http.StatusForbidden)
		return
	}

	progressTime := time.Now()

	err := coll.Update(bson.M{"_id": stepId, "device": owner}, bson.M{"$set": bson.M{"progress": stepProgress, "progress-time": progressTime}})

	if err != nil {
		rest.Error(w, "Cannot update step progress "+err.Error(), http.StatusForbidden)
		return
	}

	err = collTrails.Update(bson.M{"_id": bson.ObjectIdHex(trailId)}, bson.M{"$set": bson.M{"last-touched": progressTime}})

	if err != nil {
		// XXX: figure how to be better on error cases here...
		fmt.Printf("Error updating last-touched for trail in poststepprogress; not failing because step was written: %s\n", trailId)
	}

	w.WriteJson(stepProgress)
}

func (a *TrailsApp) get_trailsummary_one(trailId bson.ObjectId, owner string, coll *mgo.Collection) (TrailSummary, error) {

	query := bson.M{
		"trail-id": trailId,
		"owner":    owner,
		"$and": []interface{}{
			bson.D{{"progress.status", bson.M{"$ne": "DONE"}}},
		},
	}

	summary := TrailSummary{}
	steps := make([]Step, 0)

	err := coll.Find(query).Sort("-rev").Limit(1).All(&steps)
	if err != nil {
		return summary, err
	}

	if len(steps) > 0 {
		step := steps[0]
		summary.ProgressRev = step.Rev
		summary.Progress = step.StepProgress.Progress
		summary.ProgressTime = step.ProgressTime
		summary.StepTime = step.StepTime
		summary.Status = step.StepProgress.Status
		summary.StatusMsg = step.StepProgress.StatusMsg
		summary.DeviceId = step.TrailId
		summary.Device = step.Device
	}

	step := Step{}
	query = bson.M{"trail-id": trailId, "owner": owner, "progress.status": "DONE"}

	err = coll.Find(query).Sort("-rev").One(&step)
	if err != nil {
		return summary, err
	}

	summary.Rev = step.Rev
	if summary.Status == "" || summary.Rev > summary.ProgressRev {
		summary.ProgressRev = step.Rev
		summary.Progress = step.StepProgress.Progress
		summary.ProgressTime = step.ProgressTime
		summary.StepTime = step.StepTime
		summary.Status = step.StepProgress.Status
		summary.StatusMsg = step.StepProgress.StatusMsg
		summary.DeviceId = step.TrailId
		summary.Device = step.Device
	}
	return summary, nil
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

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	collSteps := a.mgoSession.DB("").C("pantahub_steps")

	if collSteps == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	collDevices := a.mgoSession.DB("").C("pantahub_devices")

	if collDevices == nil {
		rest.Error(w, "Error with Database connectivity - devices", http.StatusInternalServerError)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]
	trailId := r.PathParam("id")

	if authType != "USER" {
		rest.Error(w, "Need to be logged in as USER to get trail summary", http.StatusForbidden)
		return
	}

	summary, _ := a.get_trailsummary_one(bson.ObjectIdHex(trailId), owner.(string), collSteps)

	device := devices.Device{}
	err := collDevices.Find(bson.M{"_id": summary.DeviceId}).One(&device)
	if err != nil {
		rest.Error(w, "Error getting device record for id "+summary.DeviceId.Hex(),
			http.StatusInternalServerError)
		return
	}

	summary.DeviceNick = device.Nick

	w.WriteJson(summary)
}

//
// ## GET /trails/summary
//   get summary of all trails by the calling owner.
func (a *TrailsApp) handle_gettrailsummary(w rest.ResponseWriter, r *rest.Request) {

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in", http.StatusForbidden)
		return
	}

	collSteps := a.mgoSession.DB("").C("pantahub_steps")

	if collSteps == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	collTrails := a.mgoSession.DB("").C("pantahub_trails")

	if collTrails == nil {
		rest.Error(w, "Error with Database connectivity - trails", http.StatusInternalServerError)
		return
	}

	collDevices := a.mgoSession.DB("").C("pantahub_devices")

	if collDevices == nil {
		rest.Error(w, "Error with Database connectivity - devices", http.StatusInternalServerError)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	if authType != "USER" {
		rest.Error(w, "Need to be logged in as USER to get trail summary", http.StatusForbidden)
		return
	}

	trails := make([]Trail, 0)
	collTrails.Find(bson.M{"owner": owner}).All(&trails)

	summaries := make([]TrailSummary, len(trails))

	for i, v := range trails {
		summaries[i], _ = a.get_trailsummary_one(v.Id, owner.(string), collSteps)
		summaries[i].TrailTouchedTime = v.LastTouched

		device := devices.Device{}
		err := collDevices.Find(bson.M{"_id": summaries[i].DeviceId}).One(&device)
		if err != nil {
			rest.Error(w, "Error getting device record for id "+summaries[i].DeviceId.Hex(),
				http.StatusInternalServerError)
			return
		}
		summaries[i].DeviceNick = device.Nick
	}
	w.WriteJson(summaries)
}

// XXX:
//   finish getsteps
//   post walk
//   get walks
//   search attributes for advanced steps/walk searching inside trail
//
func New(jwtMiddleware *jwt.JWTMiddleware, session *mgo.Session) *TrailsApp {

	app := new(TrailsApp)
	app.jwt_middleware = jwtMiddleware
	app.mgoSession = session

	app.Api = rest.NewApi()

	// we dont use default stack because we dont want content type enforcement
	app.Api.Use(&rest.AccessLogApacheMiddleware{})
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

	// no authentication needed for /login
	app.Api.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			return true
		},
		IfTrue: app.jwt_middleware,
	})

	// /auth_status endpoints
	// XXX: this is all needs to be done so that paths that do not trail with /
	//      get a MOVED PERMANTENTLY error with the redir path with / like the main
	//      API routers (bad rest.MakeRouter I suspect)
	api_router, _ := rest.MakeRouter(
		rest.Get("/auth_status", handle_auth),
		rest.Get("/", app.handle_gettrails),
		rest.Post("/", app.handle_posttrail),
		rest.Get("/summary", app.handle_gettrailsummary),
		rest.Get("/:id", app.handle_gettrail),
		rest.Get("/:id/.pvrremote", app.handle_gettrailpvrinfo),
		rest.Post("/:id/steps", app.handle_poststep),
		rest.Get("/:id/steps", app.handle_getsteps),
		rest.Get("/:id/steps/:rev", app.handle_getstep),
		rest.Get("/:id/steps/:rev/.pvrremote", app.handle_getsteppvrinfo),
		rest.Get("/:id/steps/:rev/state", app.handle_getstepstate),
		rest.Put("/:id/steps/:rev/state", app.handle_putstepstate),
		rest.Put("/:id/steps/:rev/progress", app.handle_putstepprogress),
		rest.Get("/:id/summary", app.handle_gettrailstepsummary),
	)
	app.Api.SetApp(api_router)

	return app
}
