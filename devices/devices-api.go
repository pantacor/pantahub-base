//
// Copyright 2016  Alexander Sack <asac129@gmail.com>
//
package devices

import (
	"net/http"
	"time"

	"github.com/StephanDollberg/go-json-rest-middleware-jwt"
	"github.com/ant0ine/go-json-rest/rest"
	petname "github.com/dustinkirkland/golang-petname"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

type DevicesApp struct {
	jwt_middleware *jwt.JWTMiddleware
	Api            *rest.Api
	mgoSession     *mgo.Session
	mgoDb          string
}

type Device struct {
	Id           bson.ObjectId `json:"id" bson:"_id"`
	Prn          string        `json:"prn"`
	Nick         string        `json:"nick"`
	Owner        string        `json:"owner"`
	Secret       string        `json:"secret"`
	TimeCreated  time.Time     `json:"time-created"`
	TimeModified time.Time     `json:"time-modified"`
	Challenge    string        `json:"challenge"`
}

func handle_auth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

func (a *DevicesApp) handle_postdevice(w rest.ResponseWriter, r *rest.Request) {

	newDevice := Device{}

	r.DecodeJsonPayload(&newDevice)

	mgoid := bson.NewObjectId()
	newDevice.Id = mgoid
	newDevice.Prn = "prn:::devices:/" + newDevice.Id.Hex()
	newDevice.Challenge = petname.Generate(3, "-")

	jwtPayload, ok := r.Env["JWT_PAYLOAD"]

	var owner interface{}

	if ok {
		owner, ok = jwtPayload.(map[string]interface{})["prn"]
	}

	if ok {
		newDevice.Owner = owner.(string)
	} else {
		newDevice.Owner = ""
	}

	newDevice.TimeCreated = time.Now()

	if newDevice.Nick == "" {
		newDevice.Nick = petname.Generate(2, "_")
	}

	collection := a.mgoSession.DB(a.mgoDb).C("pantahub_devices")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	collection.UpsertId(mgoid, newDevice)

	w.WriteJson(newDevice)
}

func (a *DevicesApp) handle_putdevice(w rest.ResponseWriter, r *rest.Request) {

	newDevice := Device{}

	putId := r.PathParam("id")

	authId, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in with a known authentication type.", http.StatusForbidden)
		return
	}

	callerIsUser := false
	callerIsDevice := false

	if authType == "DEVICE" {
		callerIsDevice = true
	} else {
		callerIsUser = true
	}

	collection := a.mgoSession.DB(a.mgoDb).C("pantahub_devices")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	err := collection.FindId(bson.ObjectIdHex(putId)).One(&newDevice)

	prn := newDevice.Prn
	timeCreated := newDevice.TimeCreated
	owner := newDevice.Owner
	challenge := newDevice.Challenge
	challengeVal := r.FormValue("challenge")

	if err != nil {
		rest.Error(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	if callerIsDevice && newDevice.Prn != authId {
		rest.Error(w, "Not Device Accessible Resource Id", http.StatusForbidden)
		return
	}

	if callerIsUser && newDevice.Owner != "" && newDevice.Owner != authId {
		rest.Error(w, "Not User Accessible Resource Id", http.StatusForbidden)
		return
	}

	r.DecodeJsonPayload(&newDevice)

	if newDevice.Id.Hex() != putId {
		rest.Error(w, "Cannot change device Id in PUT", http.StatusForbidden)
		return
	}

	if newDevice.Prn != prn {
		rest.Error(w, "Cannot change device prn in PUT", http.StatusForbidden)
		return
	}

	if newDevice.Owner != owner {
		rest.Error(w, "Cannot change device owner in PUT", http.StatusForbidden)
		return
	}

	if newDevice.TimeCreated != timeCreated {
		rest.Error(w, "Cannot change device timeCreated in PUT", http.StatusForbidden)
		return
	}

	if newDevice.Secret == "" {
		rest.Error(w, "Empty Secret not allowed for devices in PUT", http.StatusForbidden)
		return
	}

	/* in case someone claims the device like this, update owner */
	if len(challenge) > 0 {
		if challenge == challengeVal {
			newDevice.Owner = authId.(string)
			newDevice.Challenge = ""
		} else {
			rest.Error(w, "No Access to Device", http.StatusForbidden)
			return
		}
	}

	newDevice.TimeModified = time.Now()
	collection.UpsertId(newDevice.Id, newDevice)

	w.WriteJson(newDevice)
}

func (a *DevicesApp) handle_getdevice(w rest.ResponseWriter, r *rest.Request) {

	var device Device

	mgoid := bson.ObjectIdHex(r.PathParam("id"))

	authId, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in.", http.StatusForbidden)
		return
	}

	authType, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["type"]

	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in with a known authentication type.", http.StatusForbidden)
		return
	}

	callerIsUser := false
	callerIsDevice := false

	if authType == "DEVICE" {
		callerIsDevice = true
	} else {
		callerIsUser = true
	}

	collection := a.mgoSession.DB(a.mgoDb).C("pantahub_devices")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	err := collection.FindId(mgoid).One(&device)

	if err != nil {
		rest.Error(w, "No Access", http.StatusForbidden)
		return
	}

	// XXX: fixme; needs delegation of authorization for device accessing its resources
	// could be subscriptions, but also something else
	if callerIsDevice && device.Prn != authId {
		rest.Error(w, "No Access", http.StatusForbidden)
		return
	}

	if callerIsUser && device.Owner != authId {
		rest.Error(w, "No Access", http.StatusForbidden)
		return
	}

	w.WriteJson(device)
}

type ModelError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (a *DevicesApp) handle_getdevices(w rest.ResponseWriter, r *rest.Request) {
	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		err := ModelError{}
		err.Code = http.StatusInternalServerError
		err.Message = "You need to be logged in as a USER"

		w.WriteHeader(int(err.Code))
		w.WriteJson(err)
		return
	}

	collection := a.mgoSession.DB(a.mgoDb).C("pantahub_devices")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	devices := make([]Device, 0)

	collection.Find(bson.M{"owner": owner}).All(&devices)

	w.WriteJson(devices)
}

func (a *DevicesApp) handle_deletedevice(w rest.ResponseWriter, r *rest.Request) {

	delId := r.PathParam("id")

	owner, ok := r.Env["JWT_PAYLOAD"].(map[string]interface{})["prn"]
	if !ok {
		// XXX: find right error
		rest.Error(w, "You need to be logged in as a USER", http.StatusForbidden)
		return
	}

	collection := a.mgoSession.DB(a.mgoDb).C("pantahub_devices")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	device := Device{}

	collection.FindId(bson.ObjectIdHex(delId)).One(&device)

	if device.Owner == owner {
		collection.RemoveId(bson.ObjectIdHex(delId))
	}

	w.WriteJson(device)
}

func New(jwtMiddleware *jwt.JWTMiddleware, session *mgo.Session) *DevicesApp {

	app := new(DevicesApp)
	app.jwt_middleware = jwtMiddleware
	app.mgoSession = session

	app.mgoDb = "pantahub-base"

	app.Api = rest.NewApi()
	// we dont use default stack because we dont want content type enforcement
	app.Api.Use(&rest.AccessLogApacheMiddleware{})
	app.Api.Use(rest.DefaultCommonStack...)

	// no authentication needed for /login
	app.Api.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			// post new device means to register... allow this unauthenticated
			return !(request.Method == "POST" && request.URL.Path == "/")
		},
		IfTrue: app.jwt_middleware,
	})

	// /auth_status endpoints
	api_router, _ := rest.MakeRouter(
		rest.Get("/auth_status", handle_auth),
		rest.Get("/", app.handle_getdevices),
		rest.Post("/", app.handle_postdevice),
		rest.Get("/:id", app.handle_getdevice),
		rest.Put("/:id", app.handle_putdevice),
		rest.Delete("/:id", app.handle_deletedevice),
	)
	app.Api.SetApp(api_router)

	return app
}
