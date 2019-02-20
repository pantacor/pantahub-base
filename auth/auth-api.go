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
package auth

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"github.com/fundapps/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gopkg.in/mgo.v2"
	"gopkg.in/mgo.v2/bson"
)

func init() {
	// if in production we disable all fixed accounts
	if os.Getenv("PANTAHUB_PRODUCTION") == "" {
		return
	}

	for k, v := range accounts.DefaultAccounts {
		passwordOverwrite := os.Getenv("PANTAHUB_DEMOACCOUNTS_PASSWORD_" + v.Nick)
		if passwordOverwrite == "" {
			delete(accounts.DefaultAccounts, k)
		} else {
			v.Password = passwordOverwrite
			accounts.DefaultAccounts[k] = v
		}
	}
}

func AccountToPayload(account accounts.Account) map[string]interface{} {
	result := map[string]interface{}{}

	switch account.Type {
	case accounts.ACCOUNT_TYPE_ADMIN:
		result["roles"] = "admin"
		result["type"] = "USER"
		break
	case accounts.ACCOUNT_TYPE_USER:
		result["roles"] = "admin"
		result["type"] = "USER"
		break
	case accounts.ACCOUNT_TYPE_DEVICE:
		result["roles"] = "device"
		result["type"] = "DEVICE"
		break
	case accounts.ACCOUNT_TYPE_SERVICE:
		result["roles"] = "service"
		result["type"] = "SERVICE"
		break
	default:
		panic("Must not reach this!")
	}

	result["id"] = account.Prn
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

func handle_auth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

func (a *AuthApp) handle_postaccount(w rest.ResponseWriter, r *rest.Request) {
	newAccount := accounts.Account{}

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
	newAccount.Type = accounts.ACCOUNT_TYPE_USER // XXX: need org approach too
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

	urlPrefix := utils.GetEnv(utils.ENV_PANTAHUB_SCHEME) + "://" + utils.GetEnv(utils.ENV_PANTAHUB_HOST_WWW)
	if utils.GetEnv(utils.ENV_PANTAHUB_PORT) != "" {
		urlPrefix += ":"
		urlPrefix += utils.GetEnv(utils.ENV_PANTAHUB_PORT)
	}

	utils.SendVerification(newAccount.Email, newAccount.Id.Hex(), newAccount.Challenge, urlPrefix)

	newAccount.Password = ""
	newAccount.Challenge = ""
	w.WriteJson(newAccount)
}

func (a *AuthApp) handle_getprofile(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)

	accountPrn := jwtClaims["prn"].(string)

	if accountPrn == "" {
		rest.Error(w, "Not logged in", http.StatusPreconditionFailed)
		return
	}

	var account accounts.Account
	var ok bool

	if account, ok = accounts.DefaultAccounts[accountPrn]; !ok {
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

	newAccount := accounts.Account{}

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

type codeRequest struct {
	Service string `json:"service"`
	Scopes  string `json:"scopes"`
}

type codeResponse struct {
	Code string `json:"code"`
}

func (app *AuthApp) handle_postcode(w rest.ResponseWriter, r *rest.Request) {
	var err error

	// this is the claim of the service authenticating itself
	caller := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"].(string)
	callerType := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"].(string)

	if caller == "" {
		rest.Error(w, "must be authenticated as user", http.StatusUnauthorized)
		return
	}

	if callerType != "USER" {
		rest.Error(w, "only USER's can request access codes", http.StatusForbidden)
		return
	}

	req := codeRequest{}
	err = r.DecodeJsonPayload(&req)

	if err != nil {
		rest.Error(w, "error decoding code request", http.StatusBadRequest)
		log.Println("WARNING: access code request received with wrong request body: " + err.Error())
		return
	}

	if req.Service == "" {
		rest.Error(w, "access code requested with invalid service", http.StatusBadRequest)
		return
	}

	var mapClaim jwtgo.MapClaims
	mapClaim = app.accessCodePayload(caller, req.Service, req.Scopes)
	mapClaim["exp"] = time.Now().Add(time.Minute * 5)

	response := codeResponse{}

	code := jwtgo.New(jwtgo.GetSigningMethod(app.jwt_middleware.SigningAlgorithm))
	code.Claims = mapClaim
	response.Code, err = code.SignedString(app.jwt_middleware.Key)
	w.WriteJson(response)
}

// this requests to swap access code with accesstoken
type tokenRequest struct {
	Code    string `json:"access-code"`
	Comment string `json:"comment"`
}
type tokenStore struct {
	ID      bson.ObjectId `json:"id", bson:"_id"`
	Client  string        `json:"client"`
	Owner   string        `json:"owner"`
	Comment string        `json:"comment"`
	Claims  jwtgo.Claims  `json:"jwt-claims"`
}

type tokenResult struct {
	Token string `json:"token"`
}

// handle_posttoken can be used by services to swap an accessCode to a long living accessToken.
// Payload is of type application/json and type TokenRequest
// note that tokenhandler is supposed to be called authenticated by service that wants the access
// token to be issued on his behalf
func (app *AuthApp) handle_posttoken(writer rest.ResponseWriter, r *rest.Request) {
	tokenRequest := tokenRequest{}
	err := r.DecodeJsonPayload(&tokenRequest)

	if err != nil {
		rest.Error(writer, "Failed to decode token Request", http.StatusBadRequest)
		return
	}
	// this is the claim of the service authenticating itself
	caller := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"].(string)

	log.Println("Requesting code " + tokenRequest.Code)
	// we parse the accessCode to see if we can swap it out.
	tok, err := jwtgo.Parse(tokenRequest.Code, func(token *jwtgo.Token) (interface{}, error) {
		jwtSecret := utils.GetEnv(utils.ENV_PANTAHUB_JWT_AUTH_SECRET)
		return []byte(jwtSecret), nil
	})

	if err != nil {
		log.Println("ERROR: Failed parsing the access Code " + err.Error())
		rest.Error(writer, "Failed parsing the access Code", http.StatusUnauthorized)
		return
	}

	err = tok.Claims.Valid()
	if err != nil {
		log.Println("ERROR: Failed validating the access Code claims: " + err.Error())
		rest.Error(writer, "Failed validating the access Code claims", http.StatusUnauthorized)
		return
	}

	claims := tok.Claims.(jwtgo.MapClaims)

	user := claims["approver_prn"].(string)
	userNick := claims["approver_nick"].(string)
	userType := claims["approver_type"].(string)
	userRoles := claims["approver_roles"].(string)
	service := claims["service"].(string)
	scopes := claims["scopes"].(string)
	log.Println("DEBUG: request to issue accesstoken: service=" + service + "user=" + user + " scopes=" + scopes)

	if service != caller {
		log.Println("WARNING: invalid service (" + service + " != " + caller + ") tries to swap an accesscode")
		rest.Error(writer, "invalid service ("+service+" != "+caller+") tries to swap an accesscode", http.StatusUnauthorized)
		return
	}

	token := jwtgo.New(jwtgo.GetSigningMethod(app.jwt_middleware.SigningAlgorithm))
	tokenClaims := token.Claims.(jwtgo.MapClaims)

	// lets get the standard payload for a user and modify it so its a service accesstoken
	if app.jwt_middleware.PayloadFunc != nil {
		for key, value := range app.jwt_middleware.PayloadFunc(user) {
			tokenClaims[key] = value
		}
	}

	// claim for a scoped token
	tokenClaims["token_id"] = bson.NewObjectId()
	tokenClaims["id"] = user
	tokenClaims["aud"] = service
	tokenClaims["scopes"] = scopes
	tokenClaims["prn"] = user
	tokenClaims["nick"] = userNick
	tokenClaims["roles"] = userRoles
	tokenClaims["type"] = userType

	tokenString, err := token.SignedString(app.jwt_middleware.Key)

	if err != nil {
		log.Println("WARNING: invalid service (" + service + " != " + caller + ") tries to swap an accesscode")
		rest.Error(writer, "invalid service ("+service+" != "+caller+") tries to swap an accesscode", http.StatusUnauthorized)
		return
	}

	collection := app.mgoSession.DB("").C("pantahub_oauth_accesstokens")

	if collection == nil {
		rest.Error(writer, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	tokenStore := tokenStore{
		ID:      tokenClaims["token_id"].(bson.ObjectId),
		Client:  service,
		Owner:   user,
		Comment: tokenRequest.Comment,
		Claims:  tokenClaims,
	}

	err = collection.Insert(&tokenStore)
	if collection == nil {
		rest.Error(writer, "Error storing issued token in DB", http.StatusInternalServerError)
		return
	}

	tokenResult := tokenResult{
		Token: tokenString,
	}

	writer.WriteJson(tokenResult)
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
		log.Fatalln("Error setting up index for pantahub_accounts: " + err.Error())
		return nil
	}

	index = mgo.Index{
		Key:        []string{"prn"},
		Unique:     false,
		DropDups:   false,
		Background: true, // See notes.
		Sparse:     true,
	}
	err = app.mgoSession.DB("").C("pantahub_accounts").EnsureIndex(index)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_accounts: " + err.Error())
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
		log.Fatalln("Error setting up index for pantahub_accounts: " + err.Error())
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
		log.Fatalln("Error setting up index for pantahub_accounts: " + err.Error())
		return nil
	}

	jwtMiddleware.Authenticator = func(userId string, password string) bool {

		if userId == "" || password == "" {
			return false
		}

		testUserId := "prn:pantahub.com:auth:/" + userId

		if plm, ok := accounts.DefaultAccounts[testUserId]; !ok {
			if strings.HasPrefix(userId, "prn:::devices:") {
				return app.deviceAuth(userId, password)
			} else {
				return app.accountAuth(userId, password)
			}
		} else {
			return plm.Password == password
		}
	}

	jwtMiddleware.PayloadFunc = func(userId string) map[string]interface{} {

		testUserId := "prn:pantahub.com:auth:/" + userId
		if plm, ok := accounts.DefaultAccounts[testUserId]; !ok {
			if strings.HasPrefix(userId, "prn:::devices:") {
				return app.devicePayload(userId)
			} else {
				return app.accountPayload(userId)
			}
		} else {
			return AccountToPayload(plm)
		}
	}

	app.Api = rest.NewApi()
	app.Api.Use(&rest.AccessLogJsonMiddleware{Logger: log.New(os.Stdout,
		"/auth:", log.Lshortfile)})
	app.Api.Use(&utils.AccessLogFluentMiddleware{Prefix: "auth"})
	app.Api.Use(rest.DefaultDevStack...)
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
		rest.Post("/token", app.handle_posttoken),
		rest.Post("/code", app.handle_postcode),
		rest.Get("/auth_status", handle_auth),
		rest.Get("/login", app.jwt_middleware.RefreshHandler),
		rest.Post("/accounts", app.handle_postaccount),
		rest.Get("/verify", app.handle_verify),
	)
	app.Api.SetApp(api_router)

	return app
}

func (a *AuthApp) getAccount(prnEmailNick string) (accounts.Account, error) {

	var (
		err     error
		account accounts.Account
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
		account accounts.Account
	)

	account, err = a.getAccount(idEmailNick)

	// error with db or not found -> log and fail
	if err != nil {
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

func (app *AuthApp) getAccountPayload(idEmailNick string) map[string]interface{} {
	var plm accounts.Account
	var ok, ok2 bool
	if plm, ok = accounts.DefaultAccounts[idEmailNick]; !ok {
		fullprn := "prn:pantahub.com:auth:/" + idEmailNick
		if plm, ok2 = accounts.DefaultAccounts[fullprn]; !ok && !ok2 {
			if strings.HasPrefix(idEmailNick, "prn:::devices:") {
				return app.devicePayload(idEmailNick)
			} else {
				return app.accountPayload(idEmailNick)
			}
		}
	}
	return AccountToPayload(plm)
}

func (a *AuthApp) accessCodePayload(userIdEmailNick string, serviceIdEmailNick string, scopes string) map[string]interface{} {
	var (
		err                   error
		userAccountPayload    map[string]interface{}
		serviceAccountPayload map[string]interface{}
	)

	serviceAccountPayload = a.getAccountPayload(serviceIdEmailNick)
	userAccountPayload = a.getAccountPayload(userIdEmailNick)

	// error with db or not found -> log and fail
	if err != nil {
		return nil
	}

	accessCodePayload := map[string]interface{}{}
	accessCodePayload["approver_prn"] = userAccountPayload["prn"]
	accessCodePayload["approver_nick"] = userAccountPayload["nick"]
	accessCodePayload["approver_roles"] = userAccountPayload["roles"]
	accessCodePayload["approver_type"] = userAccountPayload["type"]
	accessCodePayload["service"] = serviceAccountPayload["prn"]
	accessCodePayload["scopes"] = scopes

	return accessCodePayload
}

func (a *AuthApp) accountPayload(idEmailNick string) map[string]interface{} {

	var (
		err     error
		account accounts.Account
	)

	account, err = a.getAccount(idEmailNick)
	account.Password = ""
	account.Challenge = ""

	// error with db or not found -> log and fail
	if err != nil {
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
	c.Find(bson.M{
		"_id":     mgoId,
		"garbage": bson.M{"$ne": true},
	}).One(&device)
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
	err := c.Find(bson.M{
		"_id":     mgoId,
		"garbage": bson.M{"$ne": true},
	}).One(&device)

	if err != nil {
		return nil
	}

	val := map[string]interface{}{
		"id":    device.Prn,
		"roles": "device",
		"type":  "DEVICE",
		"prn":   device.Prn,
		"owner": device.Owner,
	}

	return val
}
