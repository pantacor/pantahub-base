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
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/x/bsonx"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
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
			log.Println("enabling default account: " + v.Nick)
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
		result["roles"] = "user"
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
	case accounts.ACCOUNT_TYPE_CLIENT:
		result["roles"] = "service"
		result["type"] = "SERVICE"
		break
	default:
		log.Println("ERROR: AccountToPayload with invalid account type: " + account.Type)
		return nil
	}

	result["id"] = account.Prn
	result["nick"] = account.Nick
	result["prn"] = account.Prn
	result["scopes"] = "prn:pantahub.com:apis:/base/all"

	return result
}

type AccountType string

const (
	ACCOUNT_TYPE_ADMIN          = AccountType("ADMIN")
	ACCOUNT_TYPE_DEVICE         = AccountType("DEVICE")
	ACCOUNT_TYPE_ORG            = AccountType("ORG")
	ACCOUNT_TYPE_SERVICE        = AccountType("SERVICE")
	ACCOUNT_TYPE_USER           = AccountType("USER")
	exchangeTokenRequiredErr    = "Exchange token is needed"
	passwordIsNeededErr         = "New password is needed"
	tokenInvalidOrExpiredErr    = "Invalid or expired token"
	emailRequiredForPasswordErr = "Email is required"
	dbConnectionErr             = "Error with Database connectivity"
	emailNotFoundErr            = "Email don't exist"
	tokenCreationErr            = "Error creating token"
	sendEmailErr                = "Error sending email"
	restorePasswordTTLUnit      = time.Minute
)

func handle_auth(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

func (a *AuthApp) handle_getaccounts(w rest.ResponseWriter, r *rest.Request) {
	var err error
	var cur *mongo.Cursor

	authInfo := utils.GetAuthInfo(r)
	r.ParseForm()
	asAdminMode := r.FormValue("asadmin")

	if asAdminMode != "" && authInfo.Roles != "admin" {
		utils.RestError(w, nil, "user has no admin role", http.StatusForbidden)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

	if collection == nil {
		utils.RestError(w, nil, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	resultSet := make([]accounts.AccountPublic, 0)
	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// NO ADMIN: FILTER
	if true && asAdminMode == "" {
		cur, err = collection.Find(ctx, bson.M{
			"$or": bson.A{
				bson.M{"prn": authInfo.Caller},
				bson.M{"owner": authInfo.Caller},
			},
			"garbage": bson.M{"$ne": true},
		}, findOptions)
	} else {
		// ADMIN: get all
		cur, err = collection.Find(ctx, bson.M{
			"garbage": bson.M{"$ne": true},
		}, findOptions)
	}

	if err != nil {
		utils.RestError(w, err, "Error on fetching accounts.", http.StatusInternalServerError)
		return
	}
	defer cur.Close(ctx)

	for cur.Next(ctx) {
		result := accounts.AccountPublic{}
		err := cur.Decode(&result)
		if err != nil {
			utils.RestError(w, err, "Cursor Decode Error", http.StatusInternalServerError)
			return
		}
		resultSet = append(resultSet, result)
	}

	w.WriteJson(&resultSet)
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

	if !newAccount.Id.IsZero() {
		rest.Error(w, "Accounts cannot have id before creation", http.StatusPreconditionFailed)
		return
	}

	passwordBcrypt, err := utils.HashPassword(newAccount.Password, utils.CryptoMethods.BCrypt)
	passwordScrypt, err := utils.HashPassword(newAccount.Password, utils.CryptoMethods.SCrypt)
	if err != nil {
		utils.RestError(w, err, err.Error(), http.StatusInternalServerError)
		return
	}
	newAccount.Password = ""
	newAccount.PasswordBcrypt = passwordBcrypt
	newAccount.PasswordScrypt = passwordScrypt

	mgoid := primitive.NewObjectID()
	ObjectID, err := primitive.ObjectIDFromHex(mgoid.Hex())
	if err != nil {
		rest.Error(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}

	newAccount.Id = ObjectID
	newAccount.Prn = "prn:::accounts:/" + newAccount.Id.Hex()
	newAccount.Challenge = utils.GenerateChallenge()
	newAccount.TimeCreated = time.Now()
	newAccount.Type = accounts.ACCOUNT_TYPE_USER // XXX: need org approach too
	newAccount.TimeModified = newAccount.TimeCreated

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	usersCount, _ := collection.CountDocuments(ctx,
		bson.M{
			"$or": []bson.M{
				{"email": newAccount.Email},
				{"nick": newAccount.Nick},
			},
		},
	)
	if usersCount > 0 {
		rest.Error(w, "Email or Nick already in use", http.StatusPreconditionFailed)
		return
	}

	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": newAccount.Id},
		bson.M{"$set": newAccount},
		updateOptions,
	)
	if err != nil {
		rest.Error(w, "Internal Error: "+err.Error(), http.StatusInternalServerError)
		return
	}

	urlPrefix := utils.GetEnv(utils.ENV_PANTAHUB_SCHEME) + "://" + utils.GetEnv(utils.ENV_PANTAHUB_HOST_WWW)
	if utils.GetEnv(utils.ENV_PANTAHUB_PORT) != "" {
		urlPrefix += ":"
		urlPrefix += utils.GetEnv(utils.ENV_PANTAHUB_PORT)
	}

	utils.SendVerification(newAccount.Email, newAccount.Nick, newAccount.Id.Hex(), newAccount.Challenge, urlPrefix)

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
		col := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cancel()
		err := col.FindOne(ctx, bson.M{"prn": accountPrn}).Decode(&account)
		// always unset credentials so we dont end up sending them out
		account.Password = ""
		account.Challenge = ""

		if err != nil {
			switch err.(type) {
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

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	r.ParseForm()
	putId := r.FormValue("id")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ObjectID, err := primitive.ObjectIDFromHex(putId)
	if err != nil {
		rest.Error(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	err = collection.FindOne(ctx,
		bson.M{
			"_id": ObjectID,
		}).
		Decode(&newAccount)
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
	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": newAccount.Id},
		bson.M{"$set": newAccount},
		updateOptions,
	)
	if err != nil {
		rest.Error(w, "Error on Updating", http.StatusInternalServerError)
		return
	}

	// always wipe secrets before sending over wire
	newAccount.Password = ""
	newAccount.Challenge = ""
	w.WriteJson(newAccount)
}

type codeRequest struct {
	Service     string `json:"service"`
	Scopes      string `json:"scopes"`
	State       string `json:"state"`
	RedirectURI string `json:"redirect_uri"`
}

type codeResponse struct {
	Code        string `json:"code"`
	Scopes      string `json:"scopes,omitempty"`
	State       string `json:"state,omitempty"`
	RedirectURI string `json:"redirect_uri,omitempty"`
}

func containsStringWithPrefix(slice []string, prefix string) bool {
	for _, v := range slice {
		if strings.HasPrefix(prefix, v) {
			return true
		}
	}
	return false
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

	// XXX: allow scopes registered as valid for service once we have scopes middleware
	if req.Scopes != "*" &&
		!strings.HasPrefix(req.Scopes, "prn:pantahub.com:apis:/base/") &&
		!strings.HasPrefix(req.Scopes, "prn:pantahub.com:apis:/fleet/") {

		rest.Error(w, "access code requested with invalid scope. During alpha, scopes '*' (all rights) is only valid scope", http.StatusBadRequest)
		return
	}

	serviceAccount, err := app.getAccount(req.Service)
	if err != nil && err != mongo.ErrNoDocuments {
		utils.RestError(w, err, "error access code creation failed to look up service", http.StatusInternalServerError)
		return
	}
	if serviceAccount.Oauth2RedirectURIs != nil && !containsStringWithPrefix(serviceAccount.Oauth2RedirectURIs, req.RedirectURI) {
		rest.Error(w, "error implicit access token failed; redirect URL does not match registered service", http.StatusBadRequest)
		return
	}

	var mapClaim jwtgo.MapClaims
	mapClaim = app.accessCodePayload(caller, req.Service, req.Scopes)

	if mapClaim == nil {
		utils.RestError(w, nil, "error decoding claims from access code", http.StatusBadRequest)
		return
	}

	mapClaim["exp"] = time.Now().Add(time.Minute * 5)

	response := codeResponse{}

	code := jwtgo.New(jwtgo.GetSigningMethod(app.jwt_middleware.SigningAlgorithm))
	code.Claims = mapClaim

	response.Code, err = code.SignedString(app.jwt_middleware.Key)
	response.Scopes = req.Scopes

	params := url.Values{}
	params.Add("code", response.Code)
	params.Add("state", req.State)
	response.RedirectURI = req.RedirectURI + "?" + params.Encode()
	w.WriteJson(response)
}

type implicitTokenRequest struct {
	codeRequest
	RedirectURI string `json:"redirect_uri"`
}

func (app *AuthApp) handle_postauthorizetoken(w rest.ResponseWriter, r *rest.Request) {
	var err error

	// this is the claim of the service authenticating itself
	caller := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"].(string)
	callerType := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["type"].(string)

	if caller == "" {
		rest.Error(w, "must be authenticated as user", http.StatusUnauthorized)
		return
	}

	if callerType != "USER" {
		rest.Error(w, "only USER's can request implicit access tokens", http.StatusForbidden)
		return
	}

	req := implicitTokenRequest{}
	err = r.DecodeJsonPayload(&req)

	if err != nil {
		rest.Error(w, "error decoding token request", http.StatusBadRequest)
		log.Println("WARNING: implicit access token request received with wrong request body: " + err.Error())
		return
	}

	if req.Service == "" {
		rest.Error(w, "implicit  access token requested with invalid service", http.StatusBadRequest)
		return
	}

	if req.Scopes != "*" &&
		!strings.HasPrefix(req.Scopes, "prn:pantahub.com:apis:/base/") &&
		!strings.HasPrefix(req.Scopes, "prn:pantahub.com:apis:/fleet/") {
		rest.Error(w, "implicit access token requested with invalid scope. During alpha, scopes '*' (all rights) or 'prn:pantahub.com:apis:/base/*' (all rights on base) or 'prn:pantahub.com:apis:/fleet/* (all rights on fleet) are only valid scopes", http.StatusBadRequest)
		return
	}

	token := jwtgo.New(jwtgo.GetSigningMethod(app.jwt_middleware.SigningAlgorithm))
	tokenClaims := token.Claims.(jwtgo.MapClaims)

	// lets get the standard payload for a user and modify it so its a service accesstoken
	if app.jwt_middleware.PayloadFunc != nil {
		for key, value := range app.jwt_middleware.PayloadFunc(caller) {
			tokenClaims[key] = value
		}
	}

	serviceAccount, err := app.getAccount(req.Service)

	if err != nil && err != mongo.ErrNoDocuments {
		log.Println("error implicit access token creation failed to look up service: " + err.Error())
		rest.Error(w, "error  implicit access token creation failed to look up service", http.StatusInternalServerError)
		return
	}

	if err == mongo.ErrNoDocuments {
		rest.Error(w, "error access token failed, due to unknown service id", http.StatusBadRequest)
		return
	}

	if serviceAccount.Oauth2RedirectURIs != nil && !containsStringWithPrefix(serviceAccount.Oauth2RedirectURIs, req.RedirectURI) {
		rest.Error(w, "error implicit access token failed; redirect URL does not match registered service", http.StatusBadRequest)
		return
	}

	tokenClaims["token_id"] = primitive.NewObjectID()
	tokenClaims["id"] = caller
	tokenClaims["aud"] = req.Service
	tokenClaims["scopes"] = req.Scopes
	tokenClaims["prn"] = caller
	tokenClaims["exp"] = time.Now().Add(app.jwt_middleware.Timeout)
	tokenString, err := token.SignedString(app.jwt_middleware.Key)

	if err != nil {
		log.Println("WARNING: error signing implicit access token for service / user / scopes(" + req.Service + " / " + caller + " / " + req.Scopes + ")")
		rest.Error(w, "error signing implicit access token for service / user / scopes("+req.Service+" / "+caller+" / "+req.Scopes+")", http.StatusUnauthorized)
		return
	}

	tokenStore := tokenStore{
		ID:      tokenClaims["token_id"].(primitive.ObjectID),
		Client:  req.Service,
		Owner:   caller,
		Comment: "",
		Claims:  tokenClaims,
	}

	collection := app.mongoClient.Database(utils.MongoDb).Collection("pantahub_oauth_accesstokens")

	if collection == nil {
		rest.Error(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}
	// XXX: prototype: for production we need to prevent posting twice!!
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = collection.InsertOne(
		ctx,
		tokenStore,
	)
	if err != nil {
		rest.Error(w, "Error inserting oauth token into database "+err.Error(), http.StatusInternalServerError)
		return
	}

	params := url.Values{}
	params.Add("token_type", "bearer")
	params.Add("access_token", tokenString)
	params.Add("expires_in", fmt.Sprintf("%d", app.jwt_middleware.Timeout/time.Second))
	params.Add("scope", req.Scopes)
	params.Add("state", req.State)

	response := tokenResponse{
		Token:       tokenString,
		RedirectURI: req.RedirectURI + "#" + params.Encode(),
		TokenType:   "bearer",
		Scopes:      req.Scopes,
	}

	w.WriteJson(response)
}

type passwordReset struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type resetPasswordClaims struct {
	Email        string    `json:"email"`
	TimeModified time.Time `json:"time-modified"`
	jwtgo.StandardClaims
}

// handle_password_reset gets the recovery token and validate it in order to overwrite the user password
func (app *AuthApp) handle_password_reset(writer rest.ResponseWriter, r *rest.Request) {
	data := passwordReset{}

	r.DecodeJsonPayload(&data)

	if data.Token == "" {
		utils.RestError(writer, nil, exchangeTokenRequiredErr, http.StatusBadRequest)
		return
	}

	if data.Password == "" {
		utils.RestError(writer, nil, passwordIsNeededErr, http.StatusBadRequest)
		return
	}

	token, err := jwtgo.ParseWithClaims(data.Token, &resetPasswordClaims{}, func(token *jwtgo.Token) (interface{}, error) {
		return app.jwt_middleware.Key, nil
	})
	if err != nil {
		utils.RestError(writer, err, tokenInvalidOrExpiredErr, http.StatusInternalServerError)
		return
	}

	claims := token.Claims.(*resetPasswordClaims)
	err = claims.Valid()
	if err != nil {
		utils.RestError(writer, err, tokenInvalidOrExpiredErr, http.StatusInternalServerError)
		return
	}

	collection := app.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	if collection == nil {
		utils.RestError(writer, nil, dbConnectionErr, http.StatusInternalServerError)
		return
	}

	filter := bson.M{
		"email":   claims.Email,
		"garbage": bson.M{"$ne": true},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	account := accounts.AccountPublic{}
	err = collection.FindOne(ctx, filter).Decode(&account)
	if err != nil {
		utils.RestError(writer, nil, emailNotFoundErr, http.StatusNotFound)
		return
	}

	if !account.TimeModified.Equal(claims.TimeModified) {
		utils.RestError(writer, nil, tokenInvalidOrExpiredErr, http.StatusBadRequest)
		return
	}

	passwordBcrypt, err := utils.HashPassword(data.Password, utils.CryptoMethods.BCrypt)
	passwordScrypt, err := utils.HashPassword(data.Password, utils.CryptoMethods.SCrypt)
	if err != nil {
		utils.RestError(writer, err, err.Error(), http.StatusInternalServerError)
		return
	}

	update := bson.M{
		"$set": bson.M{
			"password":        "",
			"password_bcrypt": passwordBcrypt,
			"password_scrypt": passwordScrypt,
			"time-modified":   time.Now(),
		},
	}

	updateOptions := options.Update()
	ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err = collection.UpdateOne(
		ctx,
		filter,
		update,
		updateOptions,
	)
	if err != nil {
		utils.RestError(writer, err, err.Error(), http.StatusInternalServerError)
		return
	}

	writer.WriteJson(true)
}

type tokenResponse struct {
	Token       string `json:"token"`
	RedirectURI string `json:"redirect_uri"`
	State       string `json:"state"`
	TokenType   string `json:"token_type"`
	Scopes      string `json:"scopes"`
}

type passwordResetRequest struct {
	Email string `json:"email"`
}

// handle_password_recovery send email with token to user in order to reset password to given user
func (app *AuthApp) handle_password_recovery(writer rest.ResponseWriter, r *rest.Request) {
	data := passwordResetRequest{}

	r.DecodeJsonPayload(&data)

	if data.Email == "" {
		utils.RestError(writer, nil, emailRequiredForPasswordErr, http.StatusPreconditionFailed)
		return
	}

	collection := app.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	if collection == nil {
		utils.RestError(writer, nil, dbConnectionErr, http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	account := accounts.AccountPublic{}
	filter := bson.M{
		"email":   data.Email,
		"garbage": bson.M{"$ne": true},
	}

	err := collection.FindOne(ctx, filter).Decode(&account)
	if err != nil {
		utils.RestError(writer, nil, emailNotFoundErr, http.StatusNotFound)
		return
	}

	restorePasswordTTL, err := strconv.Atoi(utils.GetEnv(utils.ENV_PANTAHUB_RECOVER_JWT_TIMEOUT_MINUTES))
	if err != nil {
		utils.RestError(writer, err, err.Error(), http.StatusInternalServerError)
	}

	claims := resetPasswordClaims{
		account.Email,
		account.TimeModified,
		jwtgo.StandardClaims{
			ExpiresAt: time.Now().UTC().Add(time.Duration(restorePasswordTTL) * restorePasswordTTLUnit).Unix(),
		},
	}

	token := jwtgo.NewWithClaims(jwtgo.GetSigningMethod(app.jwt_middleware.SigningAlgorithm), claims)

	tokenString, err := token.SignedString(app.jwt_middleware.Key)
	if err != nil {
		utils.RestError(writer, err, tokenCreationErr, http.StatusInternalServerError)
		return
	}

	err = utils.SendResetPasswordEmail(account.Email, account.Nick, tokenString)
	if err != nil {
		utils.RestError(writer, err, sendEmailErr, http.StatusInternalServerError)
		return
	}

	writer.WriteJson(true)
}

// this requests to swap access code with accesstoken
type tokenRequest struct {
	Code    string `json:"access-code"`
	Comment string `json:"comment"`
}

type tokenStore struct {
	ID      primitive.ObjectID     `json:"id", bson:"_id"`
	Client  string                 `json:"client"`
	Owner   string                 `json:"owner"`
	Comment string                 `json:"comment"`
	Claims  map[string]interface{} `json:"jwt-claims"`
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
	tokenClaims["token_id"] = primitive.NewObjectID()
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

	collection := app.mongoClient.Database(utils.MongoDb).Collection("pantahub_oauth_accesstokens")

	if collection == nil {
		rest.Error(writer, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	tokenStore := tokenStore{
		ID:      tokenClaims["token_id"].(primitive.ObjectID),
		Client:  service,
		Owner:   user,
		Comment: tokenRequest.Comment,
		Claims:  tokenClaims,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err = collection.InsertOne(ctx, &tokenStore)
	if err != nil {
		rest.Error(writer, "Error storing issued token in DB", http.StatusInternalServerError)
		return
	}

	tokenResult := tokenResponse{
		Token:     tokenString,
		TokenType: "bearer",
		Scopes:    scopes,
	}

	writer.WriteJson(tokenResult)
}

type AuthApp struct {
	jwt_middleware *jwt.JWTMiddleware
	Api            *rest.Api
	mongoClient    *mongo.Client
}

func New(jwtMiddleware *jwt.JWTMiddleware, mongoClient *mongo.Client) *AuthApp {

	app := new(AuthApp)
	app.jwt_middleware = jwtMiddleware
	app.mongoClient = mongoClient

	//key := flag.String("nick", "", "The field you'd like to place an index on")
	//unique := flag.Bool("unique", true, "Would you like the index to be unique?")
	//value := flag.Int("type", 1, "would you like the index to be ascending (1) or descending (-1)?")
	CreateIndexesOptions := options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions := options.IndexOptions{}
	indexOptions.SetUnique(true)
	indexOptions.SetSparse(true)
	indexOptions.SetBackground(true)

	index := mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "nick", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection := app.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	_, err := collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_accounts: " + err.Error())
		return nil
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(false)
	indexOptions.SetSparse(true)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "prn", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_accounts: " + err.Error())
		return nil
	}

	CreateIndexesOptions = options.CreateIndexesOptions{}
	CreateIndexesOptions.SetMaxTime(10 * time.Second)

	indexOptions = options.IndexOptions{}
	indexOptions.SetUnique(true)
	indexOptions.SetSparse(true)
	indexOptions.SetBackground(true)

	index = mongo.IndexModel{
		Keys: bsonx.Doc{
			{Key: "email", Value: bsonx.Int32(1)},
		},
		Options: &indexOptions,
	}
	collection = app.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	_, err = collection.Indexes().CreateOne(context.Background(), index, &CreateIndexesOptions)
	if err != nil {
		log.Fatalln("Error setting up index for pantahub_accounts: " + err.Error())
		return nil
	}
	jwtMiddleware.Authenticator = func(userId string, password string) bool {

		var loginUser string

		if userId == "" || password == "" {
			return false
		}

		userTup := strings.SplitN(userId, "==>", 2)
		if len(userTup) > 1 {
			loginUser = userTup[0]
		} else {
			loginUser = userId
		}

		testUserID := loginUser
		if !strings.HasPrefix(loginUser, "prn:") {
			testUserID = "prn:pantahub.com:auth:/" + loginUser
		}

		plm, ok := accounts.DefaultAccounts[testUserID]
		if !ok {
			if strings.HasPrefix(loginUser, "prn:::devices:") {
				return app.deviceAuth(loginUser, password)
			}

			return app.accountAuth(loginUser, password)
		}

		return plm.Password == password
	}

	jwtMiddleware.PayloadFunc = func(userId string) map[string]interface{} {

		var loginUser, callUser string
		var payload map[string]interface{}

		userTup := strings.SplitN(userId, "==>", 2)
		if len(userTup) > 1 {
			loginUser = userTup[0]
			callUser = userTup[1]
		} else {
			loginUser = userId
		}

		testUserID := loginUser
		if !strings.HasPrefix(loginUser, "prn:") {
			testUserID = "prn:pantahub.com:auth:/" + loginUser
		}
		if plm, ok := accounts.DefaultAccounts[testUserID]; !ok {
			if strings.HasPrefix(userId, "prn:::devices:") {
				payload = app.devicePayload(loginUser)
			} else {
				payload = app.accountPayload(loginUser)
			}
		} else {
			payload = AccountToPayload(plm)
		}

		if payload == nil {
			return nil
		}

		if callUser != "" {
			callPayload := jwtMiddleware.PayloadFunc(callUser)
			callPayload["id"] = payload["id"].(string) + "==>" + callPayload["id"].(string)
			payload["call-as"] = callPayload
		}

		return payload
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
				!(request.URL.Path == "/verify" && request.Method == "GET") &&
				!(request.URL.Path == "/recover" && request.Method == "POST") &&
				!(request.URL.Path == "/password" && request.Method == "POST")
		},
		IfTrue: app.jwt_middleware,
	})

	// no authentication needed for /login
	app.Api.Use(&rest.IfMiddleware{
		Condition: func(request *rest.Request) bool {
			return request.URL.Path != "/login" &&
				!(request.URL.Path == "/accounts" && request.Method == "POST") &&
				!(request.URL.Path == "/verify" && request.Method == "GET") &&
				!(request.URL.Path == "/recover" && request.Method == "POST") &&
				!(request.URL.Path == "/password" && request.Method == "POST")
		},
		IfTrue: &utils.AuthMiddleware{},
	})

	// /login /auth_status and /refresh_token endpoints
	api_router, _ := rest.MakeRouter(
		rest.Get("/", app.handle_getprofile),
		rest.Post("/login", app.jwt_middleware.LoginHandler),
		rest.Post("/token", app.handle_posttoken),
		rest.Post("/authorize", app.handle_postauthorizetoken),
		rest.Post("/code", app.handle_postcode),
		rest.Get("/auth_status", handle_auth),
		rest.Get("/login", app.jwt_middleware.RefreshHandler),
		rest.Get("/accounts", app.handle_getaccounts),
		rest.Post("/accounts", app.handle_postaccount),
		rest.Get("/verify", app.handle_verify),
		rest.Post("/recover", app.handle_password_recovery),
		rest.Post("/password", app.handle_password_reset),
	)
	app.Api.SetApp(api_router)

	return app
}

func (a *AuthApp) getAccount(prnEmailNick string) (accounts.Account, error) {

	var (
		err     error
		account accounts.Account
	)
	if strings.HasPrefix(prnEmailNick, "prn:::devices:") {
		return account, errors.New("getAccount does not serve device accounts")
	}

	var ok, ok2 bool
	if account, ok = accounts.DefaultAccounts[prnEmailNick]; !ok {
		fullprn := "prn:pantahub.com:auth:/" + prnEmailNick
		account, ok2 = accounts.DefaultAccounts[fullprn]
	}

	if ok || ok2 {
		return account, nil
	}

	c := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

	// we accept three variants to identify the account:
	//  - id (pure and with prn format
	//  - email
	//  - nick
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if utils.IsEmail(prnEmailNick) {
		err = c.FindOne(ctx, bson.M{"email": prnEmailNick}).Decode(&account)
	} else if utils.IsNick(prnEmailNick) {
		err = c.FindOne(ctx, bson.M{"nick": prnEmailNick}).Decode(&account)
	} else {
		err = c.FindOne(ctx, bson.M{"prn": prnEmailNick}).Decode(&account)
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
	if utils.CheckPasswordHash(secret, account.PasswordBcrypt, utils.CryptoMethods.BCrypt) {
		return true
	}
	if account.Password != "" && secret == account.Password {
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
		userAccountPayload    map[string]interface{}
		serviceAccountPayload map[string]interface{}
	)

	serviceAccountPayload = a.getAccountPayload(serviceIdEmailNick)
	userAccountPayload = a.getAccountPayload(userIdEmailNick)

	// error with db or not found -> log and fail
	if serviceAccountPayload == nil {
		return nil
	}

	if userAccountPayload == nil {
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

	c := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	id := utils.PrnGetId(deviceId)
	mgoId, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return false
	}

	device := devices.Device{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deviceID, err := primitive.ObjectIDFromHex(mgoId.Hex())
	if err != nil {
		return false
	}
	err = c.FindOne(ctx, bson.M{
		"_id":     deviceID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&device)
	if err != nil {
		return false
	}
	if secret == device.Secret {
		return true
	}
	return false
}

func (a *AuthApp) devicePayload(deviceId string) map[string]interface{} {

	c := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices")

	id := utils.PrnGetId(deviceId)
	mgoId, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil
	}

	device := devices.Device{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	deviceID, err := primitive.ObjectIDFromHex(mgoId.Hex())
	if err != nil {
		return nil
	}
	err = c.FindOne(ctx, bson.M{
		"_id":     deviceID,
		"garbage": bson.M{"$ne": true},
	}).Decode(&device)
	if err != nil {
		return nil
	}

	val := map[string]interface{}{
		"id":     device.Prn,
		"roles":  "device",
		"type":   "DEVICE",
		"prn":    device.Prn,
		"owner":  device.Owner,
		"scopes": "prn:pantahub.com:apis:/base/all",
	}

	return val
}
