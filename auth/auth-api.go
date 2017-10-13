//
// Copyright 2016,2017  Pantacor Ltd.
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
package auth

import (
	"strings"

	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"

	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/StephanDollberg/go-json-rest-middleware-jwt"
	"github.com/ant0ine/go-json-rest/rest"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

var accounts = map[string]Account{
	"prn:pantahub.com:auth:/admin": Account{
		Type:         ACCOUNT_TYPE_ADMIN,
		Prn:          "prn:pantahub.com:auth:/admin",
		Nick:         "admin",
		Email:        "no-reply@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "admin",
	},
	"prn:pantahub.com:auth:/user1": Account{
		Type:         ACCOUNT_TYPE_USER,
		Prn:          "prn:pantahub.com:auth:/user1",
		Nick:         "user1",
		Email:        "no-reply@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "user1",
	},
	"prn:pantahub.com:auth:/user2": Account{
		Type:         ACCOUNT_TYPE_USER,
		Prn:          "prn:pantahub.com:auth:/user2",
		Nick:         "user2",
		Email:        "no-reply@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "user2",
	},
	"prn:pantahub.com:auth:/user3": Account{
		Type:         ACCOUNT_TYPE_USER,
		Prn:          "prn:pantahub.com:auth:/user3",
		Nick:         "user3",
		Email:        "no-reply@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "user3",
	},
	"prn:pantahub.com:auth:/examples": Account{
		Type:         ACCOUNT_TYPE_USER,
		Prn:          "prn:pantahub.com:auth:/user3",
		Nick:         "examples",
		Email:        "no-reply@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "examples",
	},
	"prn:pantahub.com:auth:/device1": Account{
		Type:         ACCOUNT_TYPE_DEVICE,
		Prn:          "prn:pantahub.com:auth:/device1",
		Nick:         "device1",
		Email:        "no-reply@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "device1",
	},
	"prn:pantahub.com:auth:/device2": Account{
		Type:         ACCOUNT_TYPE_DEVICE,
		Prn:          "prn:pantahub.com:auth:/device2",
		Nick:         "device2",
		Email:        "no-reply@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "device2",
	},
	"prn:pantahub.com:auth:/service1": Account{
		Type:         ACCOUNT_TYPE_SERVICE,
		Prn:          "prn:pantahub.com:auth:/service1",
		Nick:         "service1",
		Email:        "no-reply@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "service1",
	},
	"prn:pantahub.com:auth:/service2": Account{
		Type:         ACCOUNT_TYPE_SERVICE,
		Prn:          "prn:pantahub.com:auth:/service2",
		Nick:         "service2",
		Email:        "no-reply@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "service2",
	},
	"prn:pantahub.com:auth:/service3": Account{
		Type:         ACCOUNT_TYPE_SERVICE,
		Prn:          "prn:pantahub.com:auth:/service3",
		Nick:         "service3",
		Email:        "no-reply@accounts.pantahub.com",
		TimeCreated:  time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2016, time.October, 1, 0, 0, 0, 0, time.UTC),
		Password:     "service3",
	},
}

func AccountToPayload(account Account) map[string]interface{} {
	result := map[string]interface{}{}

	switch account.Type {
	case ACCOUNT_TYPE_ADMIN:
		result["roles"] = "admin"
		result["type"] = "USER"
		break
	case ACCOUNT_TYPE_USER:
		result["roles"] = "admin"
		result["type"] = "USER"
		break
	case ACCOUNT_TYPE_DEVICE:
		result["roles"] = "device"
		result["type"] = "DEVICE"
		break
	case ACCOUNT_TYPE_SERVICE:
		result["roles"] = "service"
		result["type"] = "SERVICE"
		break
	default:
		panic("Must not reach this!")
		return nil
	}

	result["nick"] = account.Nick
	result["prn"] = account.Prn

	return result
}

type AccountType string

const (
	ACCOUNT_TYPE_ADMIN   = AccountType("ADMIN")
	ACCOUNT_TYPE_DEVICE  = AccountType("DEVICE")
	ACCOUNT_TYPE_ORG     = AccountType("ORG")
	ACCOUNT_TYPE_SERVICE = AccountType("SERVICE")
	ACCOUNT_TYPE_USER    = AccountType("USER")
)

type Account struct {
	Id bson.ObjectId `json:"-" bson:"_id"`

	Type  AccountType `json:"type" bson:"type"`
	Email string      `json:"email" bson:"email"`
	Nick  string      `json:"nick" bson:"nick"`
	Prn   string      `json:"prn" bson:"prn"`

	Password  string `json:"password,omitempty" bson:"password"`
	Challenge string `json:"challenge,omitempty" bson:"challenge"`

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

	urlPrefix := utils.GetEnv(utils.ENV_PANTAHUB_SCHEME) + "://" + utils.GetEnv(utils.ENV_PANTAHUB_HOST)
	if utils.GetEnv(utils.ENV_PANTAHUB_PORT) != "" {
		urlPrefix += ":"
		urlPrefix += utils.GetEnv(utils.ENV_PANTAHUB_PORT)
	}

	utils.SendVerification(newAccount.Email, newAccount.Id.Hex(), newAccount.Challenge, urlPrefix)
	w.WriteJson(newAccount)
}

func (a *AuthApp) handle_getprofile(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"].(map[string]interface{})

	accountPrn := jwtClaims["prn"].(string)
	fmt.Println("ACCOUNT: " + accountPrn)

	if accountPrn == "" {
		rest.Error(w, "Not logged in", http.StatusPreconditionFailed)
		return
	}

	var account Account
	var ok bool

	if account, ok = accounts[accountPrn]; !ok {
		col := a.mgoSession.DB("").C("pantahub_accounts")

		err := col.Find(bson.M{"prn": accountPrn}).One(&account)
		// always unset credentials so we dont end up sending them out
		account.Password = ""
		account.Challenge = ""

		if err != nil {
			switch err.(type) {
			case *mgo.QueryError:
				qErr := err.(*mgo.QueryError)
				rest.Error(w, "Query Error: "+qErr.Error(), http.StatusInternalServerError)
				break
			default:
				rest.Error(w, "Account "+err.Error(), http.StatusInternalServerError)
				break
			}
		}
	}

	w.WriteJson(account)
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

	// always wipe secrets before sending over wire
	newAccount.Password = ""
	newAccount.Challenge = ""
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

	index = mgo.Index{
		Key:        []string{"nick"},
		Unique:     true,
		Background: true,
		Sparse:     false,
	}

	err = app.mgoSession.DB("").C("pantahub_accounts").EnsureIndex(index)
	if err != nil {
		log.Println("Error setting up index for pantahub_accounts: " + err.Error())
		return nil
	}

	jwtMiddleware.Authenticator = func(userId string, password string) bool {
		if userId == "" || password == "" {
			return false
		}

		if !strings.HasPrefix(userId, "prn:") {
			userId = "prn:pantahub.com:auth:/" + userId
		}

		if plm, ok := accounts[userId]; !ok {
			if strings.HasPrefix(userId, "prn:::devices:") {
				return app.deviceAuth(userId, password)
			} else {
				return app.accountAuth(userId, password)
			}
		} else {
			return plm.Password == password
		}
		return false
	}

	jwtMiddleware.PayloadFunc = func(userId string) map[string]interface{} {

		if !strings.HasPrefix(userId, "prn:") {
			userId = "prn:pantahub.com:auth:/" + userId
		}

		if plm, ok := accounts[userId]; !ok {
			if strings.HasPrefix(userId, "prn:::devices:") {
				return app.devicePayload(userId)
			} else {
				return app.userPayload(userId)
			}
		} else {
			return AccountToPayload(plm)
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
		rest.Get("/", app.handle_getprofile),
		rest.Post("/login", app.jwt_middleware.LoginHandler),
		rest.Get("/auth_status", handle_auth),
		rest.Get("/login", app.jwt_middleware.RefreshHandler),
		rest.Post("/accounts", app.handle_postaccount),
		rest.Get("/verify", app.handle_verify),
	)
	app.Api.SetApp(api_router)

	return app
}

func (a *AuthApp) getAccount(prnEmailNick string) (Account, error) {

	var (
		err     error
		account Account
	)

	c := a.mgoSession.DB("").C("pantahub_accounts")

	// we accept three variants to identify the account:
	//  - id (pure and with prn format
	//  - email
	//  - nick
	if utils.IsEmail(prnEmailNick) {
		err = c.Find(bson.M{"email": prnEmailNick}).One(&account)
	} else if utils.IsNick(prnEmailNick) {
		err = c.Find(bson.M{"nick": prnEmailNick}).One(&account)
	} else {
		err = c.Find(bson.M{"prn": prnEmailNick}).One(&account)
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

func (a *AuthApp) userPayload(idEmailNick string) map[string]interface{} {

	var (
		err     error
		account Account
	)

	account, err = a.getAccount(idEmailNick)
	account.Password = ""
	account.Challenge = ""

	// error with db or not found -> log and fail
	if err != nil {
		fmt.Println("ERROR finding account: " + err.Error())
		return nil
	}

	val := AccountToPayload(account)

	return val
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

func (a *AuthApp) devicePayload(deviceId string) map[string]interface{} {

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

	return val
}
