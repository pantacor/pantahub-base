//
// Copyright 2016  Alexander Sack <asac129@gmail.com>
//
package auth

import (
	"strings"

	"pantahub-base/devices"
	"pantahub-base/utils"

	"fmt"
	"net/http"
	"time"

	"github.com/StephanDollberg/go-json-rest-middleware-jwt"
	"github.com/ant0ine/go-json-rest/rest"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

var passwords = map[string]string{
	"admin":    "admin",
	"user1":    "user1",
	"user2":    "user2",
	"service1": "service1",
	"service2": "service2",
	"service3": "service3",
	"device1":  "device1",
	"device2":  "device2",
}

var payloads = map[string]map[string]interface{}{
	"admin": map[string]interface{}{
		"roles": "admin",
		"type":  "USER",
		"prn":   "prn:pantahub.com:auth:/admin",
	},
	"user1": map[string]interface{}{
		"roles": "user",
		"type":  "USER",
		"nick":  "user1",
		"prn":   "prn:pantahub.com:auth:/user1",
	},
	"user2": map[string]interface{}{
		"roles": "user",
		"type":  "USER",
		"nick":  "user2",
		"prn":   "prn:pantahub.com:auth:/user2",
	},
	"service1": map[string]interface{}{
		"roles": "service",
		"type":  "SERVICE",
		"prn":   "prn:pantahub.com:auth:/service1",
	},
	"service2": map[string]interface{}{
		"roles": "service",
		"type":  "SERVICE",
		"prn":   "prn:pantahub.com:auth:/service2",
	},
	"service3": map[string]interface{}{
		"roles": "user",
		"type":  "SERVICE",
		"prn":   "prn:pantahub.com:auth:/service3",
	},
	"device1": map[string]interface{}{
		"roles": "device",
		"type":  "DEVICE",
		"prn":   "prn:pantahub.com:auth:/device1",
		"owner": "prn:pantahub.com:auth:/user1",
	},
	"device2": map[string]interface{}{
		"roles": "device",
		"type":  "DEVICE",
		"prn":   "prn:pantahub.com:auth:/device2",
		"owner": "prn:pantahub.com:auth:/user2",
	},
}

type AccountType string

const (
	ACCOUNT_TYPE_USER = AccountType("USER")
	ACCOUNT_TYPE_ORG  = AccountType("ORG")
)

type Account struct {
	Id bson.ObjectId `json:"id" bson:"_id"`

	Type  AccountType `json:"type" bson:"type"`
	Email string      `json:"email" bson:"email"`
	Nick  string      `json:"nick" bson:"nick"`
	Prn   string      `json:"prn" bson:"prn"`

	Password  string `json:"password" bson:"password"`
	Challenge string `json:"-" bson:"challenge"`

	TimeCreated  time.Time `json:"time-created" bson:"time-created"`
	TimeModified time.Time `json:"time-modified" bson:"time-modified"`
}

func handle_auth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

func (a *AuthApp) handle_postaccount(w rest.ResponseWriter, r *rest.Request) {
	newAccount := Account{}

	r.DecodeJsonPayload(&newAccount)

	if newAccount.Email == "" {
		rest.Error(w, "Accounts must have an email address", http.StatusPreconditionFailed)
		return
	}

	if newAccount.Password == "" {
		rest.Error(w, "Accounts must have a password set", http.StatusPreconditionFailed)
		return
	}

	if newAccount.Nick == "" {
		rest.Error(w, "Accounts must have a nick set", http.StatusPreconditionFailed)
		return
	}

	if newAccount.Id != "" {
		rest.Error(w, "Accounts cannot have id before creation", http.StatusPreconditionFailed)
		return
	}

	newAccount.Id = bson.NewObjectId()
	newAccount.Prn = "prn:::accounts:/" + newAccount.Id.Hex()
	newAccount.Challenge = utils.GenerateChallenge()
	newAccount.TimeCreated = time.Now()
	newAccount.Type = ACCOUNT_TYPE_USER // XXX: need org approach too
	newAccount.TimeModified = newAccount.TimeCreated

	collection := a.mgoSession.DB("").C("pantahub_accounts")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	_, err := collection.UpsertId(newAccount.Id, newAccount)

	if err != nil {
		if mgo.IsDup(err) {
			rest.Error(w, "Email or Nick already in use", http.StatusPreconditionFailed)
		} else {
			rest.Error(w, "Internal Error: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}

	urlPrefix := utils.GetEnv("PANTAHUB_SCHEME") + "://" + utils.GetEnv("PANTAHUB_HOST")
	if utils.GetEnv("PANTAHUB_PORT") != "" {
		urlPrefix += ":"
		urlPrefix += utils.GetEnv("PANTAHUB_PORT")
	}

	utils.SendVerification(newAccount.Email, newAccount.Id.Hex(), newAccount.Challenge, urlPrefix)

	w.WriteJson(newAccount)
}

func (a *AuthApp) handle_verify(w rest.ResponseWriter, r *rest.Request) {

	newAccount := Account{}

	collection := a.mgoSession.DB("").C("pantahub_accounts")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	r.ParseForm()
	putId := r.FormValue("id")

	err := collection.FindId(bson.ObjectIdHex(putId)).One(&newAccount)

	if err != nil {
		rest.Error(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	challenge := newAccount.Challenge
	challengeVal := r.FormValue("challenge")

	/* in case someone claims the device like this, update owner */
	if len(challenge) > 0 {
		if challenge == challengeVal {
			newAccount.Challenge = ""
		} else {
			rest.Error(w, "Invalid Challenge (wrong, used or never existed)", http.StatusPreconditionFailed)
			return
		}
	} else {
		rest.Error(w, "Invalid Challenge (wrong, used or never existed)", http.StatusPreconditionFailed)
		return
	}

	newAccount.TimeModified = time.Now()
	collection.UpsertId(newAccount.Id, newAccount)

	w.WriteJson(newAccount)
}

type AuthApp struct {
	jwt_middleware *jwt.JWTMiddleware
	Api            *rest.Api
	mgoSession     *mgo.Session
}

func New(jwtMiddleware *jwt.JWTMiddleware, session *mgo.Session) *AuthApp {

	app := new(AuthApp)
	app.jwt_middleware = jwtMiddleware
	app.mgoSession = session

	index := mgo.Index{
		Key:        []string{"nick"},
		Unique:     true,
		DropDups:   true,
		Background: true, // See notes.
		Sparse:     true,
	}
	err := app.mgoSession.DB("").C("pantahub_accounts").EnsureIndex(index)
	if err != nil {
		fmt.Println("Error setting up index for pantahub_accounts: " + err.Error())
		return nil
	}

	index = mgo.Index{
		Key:        []string{"email"},
		Unique:     true,
		DropDups:   true,
		Background: true, // See notes.
		Sparse:     true,
	}
	err = app.mgoSession.DB("").C("pantahub_accounts").EnsureIndex(index)
	if err != nil {
		fmt.Println("Error setting up index for pantahub_accounts: " + err.Error())
		return nil
	}

	jwtMiddleware.Authenticator = func(userId string, password string) bool {
		if userId == "" || password == "" {
			return false
		}
		if passwords[userId] != "" && passwords[userId] == password {
			return true
		}
		if strings.HasPrefix(userId, "prn:::devices:") {
			return app.deviceAuth(userId, password)
		} else {
			return app.accountAuth(userId, password)
		}
		return false
	}

	jwtMiddleware.PayloadFunc = func(userId string) map[string]interface{} {

		if plm, ok := payloads[userId]; !ok {
			if strings.HasPrefix(userId, "prn:::devices:") {
				return *app.devicePayload(userId)
			} else {
				return *app.accountPayload(userId)
			}
		} else {
			return plm
		}
		return map[string]interface{}{}
	}

	app.Api = rest.NewApi()
	app.Api.Use(rest.DefaultDevStack...)
	app.Api.Use(&rest.CorsMiddleware{
		RejectNonCorsRequests: false,
		OriginValidator: func(origin string, request *rest.Request) bool {
			return true
		},
		AllowedMethods: []string{"GET", "POST", "PUT", "OPTIONS"},
		AllowedHeaders: []string{
			"Accept", "Content-Type", "X-Custom-Header", "Origin", "Authorization"},
		AccessControlAllowCredentials: true,
		AccessControlMaxAge:           3600,
	})

	// no authentication needed for /login
	app.Api.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			return request.URL.Path != "/login" &&
				!(request.URL.Path == "/accounts" && request.Method == "POST") &&
				!(request.URL.Path == "/verify" && request.Method == "GET")
		},
		IfTrue: app.jwt_middleware,
	})

	// /login /auth_status and /refresh_token endpoints
	api_router, _ := rest.MakeRouter(
		rest.Post("/login", app.jwt_middleware.LoginHandler),
		rest.Get("/auth_status", handle_auth),
		rest.Get("/login", app.jwt_middleware.RefreshHandler),
		rest.Post("/accounts", app.handle_postaccount),
		rest.Get("/verify", app.handle_verify),
	)
	app.Api.SetApp(api_router)

	return app
}

func (a *AuthApp) getAccount(idEmailNick string) (Account, error) {

	var (
		err     error
		account Account
	)

	c := a.mgoSession.DB("").C("pantahub_accounts")

	// we accept three variants to identify the account:
	//  - id (pure and with prn format
	//  - email
	//  - nick
	if utils.IsEmail(idEmailNick) {
		err = c.Find(bson.M{"email": idEmailNick}).One(&account)
	} else if utils.IsNick(idEmailNick) {
		err = c.Find(bson.M{"nick": idEmailNick}).One(&account)
	} else {
		id := utils.PrnGetId(idEmailNick)
		mgoId := bson.ObjectIdHex(id)
		err = c.FindId(mgoId).One(&account)
	}

	return account, err
}

func (a *AuthApp) accountAuth(idEmailNick string, secret string) bool {

	var (
		err     error
		account Account
	)

	account, err = a.getAccount(idEmailNick)

	// error with db or not found -> log and fail
	if err != nil {
		fmt.Println("ERROR finding account: " + err.Error())
		return false
	}

	// account has still a challenge -> not activated -> fail to login
	if account.Challenge != "" {
		return false
	}

	// account has same password as the secret provided to func call -> success
	if secret == account.Password {
		return true
	}

	// fail by default.
	return false
}

func (a *AuthApp) accountPayload(idEmailNick string) *map[string]interface{} {

	var (
		err     error
		account Account
	)

	account, err = a.getAccount(idEmailNick)

	// error with db or not found -> log and fail
	if err != nil {
		fmt.Println("ERROR finding account: " + err.Error())
		return nil
	}

	val := map[string]interface{}{
		"roles": "users",
		"type":  account.Type,
		"nick":  account.Nick,
		"prn":   account.Prn,
	}

	return &val
}

func (a *AuthApp) deviceAuth(deviceId string, secret string) bool {

	c := a.mgoSession.DB("").C("pantahub_devices")

	id := utils.PrnGetId(deviceId)
	mgoId := bson.ObjectIdHex(id)

	device := devices.Device{}
	c.FindId(mgoId).One(&device)
	if secret == device.Secret {
		return true
	}
	return false
}

func (a *AuthApp) devicePayload(deviceId string) *map[string]interface{} {

	c := a.mgoSession.DB("").C("pantahub_devices")

	id := utils.PrnGetId(deviceId)
	mgoId := bson.ObjectIdHex(id)

	device := devices.Device{}
	err := c.FindId(mgoId).One(&device)

	if err != nil {
		return nil
	}

	val := map[string]interface{}{
		"roles": "device",
		"type":  "DEVICE",
		"prn":   device.Prn,
		"owner": device.Owner,
	}

	return &val
}
