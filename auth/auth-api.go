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

// Package auth package to manage extensions of the oauth protocol
package auth

import (
	"context"
	"log"
	"net/http"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/ant0ine/go-json-rest/rest"
	jwtgo "github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/accounts/accountsdata"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/mongo"
)

type accountClaims struct {
	Exp     string `json:"exp"`
	ID      string `json:"id"`
	Nick    string `json:"nick"`
	OrigIat string `json:"orig_iat"`
	Prn     string `json:"prn"`
	Roles   string `json:"roles"`
	Scopes  string `json:"scopes"`
	Type    string `json:"type"`
}

// handleAuthStatus Get JWT claims from Authorization header
// @Summary Get JWT claims from Authorization header
// @Description Get JWT claims from Authorization header
// @Accept  json
// @Produce  json
// @Tags auth
// @Security ApiKeyAuth
// @Success 200 {object} accountClaims
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 404 {object} utils.RError "Account not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /auth/auth_status [get]
func handleAuthStatus(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"]
	w.WriteJson(jwtClaims)
}

// handleGetAccounts Get list of accounts
// @Summary Get list of accounts
// @Description Get list of accounts
// @Accept  json
// @Produce  json
// @Tags auth
// @Security ApiKeyAuth
// @Success 200 {array} accounts.Account
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 403 {object} utils.RError "user has no admin role"
// @Failure 404 {object} utils.RError "Account not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /auth/accounts [get]
func (a *App) handleGetAccounts(w rest.ResponseWriter, r *rest.Request) {
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
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
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

// handlePostSession Create an anonymous "session" account without password
// @Summary Create a new (anon) session account
// @Description Create a new (anon) session account
// @Accept  json
// @Produce  json
// @Tags auth
// @Success 200 {object} accounts.Account
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /auth/sessions [post]
func (a *App) handlePostSession(w rest.ResponseWriter, r *rest.Request) {

	sessionAccount := accounts.Account{}
	sessionAccount.ID = primitive.NewObjectID()
	sessionAccount.Type = accounts.AccountTypeSessionUser
	sessionAccount.Nick = "__SESSION__" + sessionAccount.ID.Hex()
	sessionAccount.Email = sessionAccount.Nick + "@sessions.mail.pantahub.com"
	sessionAccount.Password = ""
	sessionAccount.Prn = "prn:::sessions:/" + sessionAccount.ID.Hex()
	sessionAccount.Challenge = ""
	sessionAccount.TimeCreated = time.Now()
	sessionAccount.TimeModified = sessionAccount.TimeCreated

	opts := options.InsertOneOptions{}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	_, err := collection.InsertOne(
		ctx,
		sessionAccount,
		&opts,
	)
	if err != nil {
		utils.RestError(w, err, err.Error(), http.StatusInternalServerError)
		return
	}

	token := jwtgo.New(jwtgo.GetSigningMethod(a.jwtMiddleware.SigningAlgorithm))
	tokenClaims := token.Claims.(jwtgo.MapClaims)

	// lets get the standard payload for a user and modify it so its a service accesstoken
	if a.jwtMiddleware.PayloadFunc != nil {
		for key, value := range a.jwtMiddleware.PayloadFunc(sessionAccount.Prn) {
			tokenClaims[key] = value
		}
	}

	tokenClaims["id"] = sessionAccount.Nick
	tokenClaims["exp"] = time.Now().Add(a.jwtMiddleware.Timeout).Unix()
	if a.jwtMiddleware.MaxRefresh != 0 {
		tokenClaims["orig_iat"] = time.Now().Unix()
	}

	tokenString, err := token.SignedString(a.jwtMiddleware.Key)

	if err != nil {
		utils.RestErrorWrapper(w, "error creating one time token "+err.Error(), http.StatusInternalServerError)
		return
	}

	sessionAccount.Password = ""
	sessionAccount.Challenge = ""

	w.WriteJson(bson.M{"token": tokenString})
}

// handlePostAccount Create a new account
// @Summary Create a new account
// @Description Create a new account
// @Accept  json
// @Produce  json
// @Tags auth
// @Param body body authmodels.AccountCreationPayload true "Account Payload"
// @Success 200 {object} accounts.Account
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 412 {object} utils.RError "Invalid payload"
// @Failure 404 {object} utils.RError "Account not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /auth/accounts [post]
func (a *App) handlePostAccount(w rest.ResponseWriter, r *rest.Request) {
	newAccount := authmodels.AccountCreationPayload{}

	r.DecodeJsonPayload(&newAccount)

	// if encrypted account data exist decryted and continue with validation
	if newAccount.EncryptedAccount != "" {
		err := utils.ParseJWE(newAccount.EncryptedAccount, &newAccount.Account)
		if err != nil {
			utils.RestError(w, err, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	if newAccount.Email == "" {
		utils.RestError(w, nil, "Accounts must have an email address", http.StatusPreconditionFailed)
		return
	}

	if newAccount.Password == "" {
		utils.RestError(w, nil, "Accounts must have a password set", http.StatusPreconditionFailed)
		return
	}

	if newAccount.Nick == "" {
		utils.RestError(w, nil, "Accounts must have a nick set", http.StatusPreconditionFailed)
		return
	}

	if !utils.IsNick(newAccount.Nick) {
		utils.RestError(w, nil, "Accounts must have a a valid nick", http.StatusPreconditionFailed)
		return
	}

	if !newAccount.ID.IsZero() {
		utils.RestError(w, nil, "Accounts cannot have id before creation", http.StatusPreconditionFailed)
		return
	}

	// Validate if user already exist
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

	if collection == nil {
		utils.RestError(w, nil, "Error with Database connectivity", http.StatusInternalServerError)
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
		utils.RestErrorUser(w, nil, "Email or Nick already in use", http.StatusPreconditionFailed)
		return
	}

	// if account creation doesn't have captcha encrypt data and send a redirect link to finish the process
	useCaptcha := utils.GetEnv(utils.EnvPantahubUseCaptcha) == "true"
	if newAccount.Captcha == "" && useCaptcha {
		response, err := handleGetEncryptedAccount(&newAccount)
		if err != nil {
			utils.RestError(w, err, err.Error(), http.StatusInternalServerError)
		}
		w.WriteJson(response)
		return
	}

	if useCaptcha {
		validCaptcha, err := utils.VerifyReCaptchaToken(newAccount.Captcha)
		if err != nil {
			utils.RestError(w, err, err.Error(), http.StatusPreconditionFailed)
			return
		}
		if !validCaptcha {
			utils.RestError(w, nil, "Invalid captcha", http.StatusPreconditionFailed)
			return
		}
	}

	passwordBcrypt, err := utils.HashPassword(newAccount.Password, utils.CryptoMethods.BCrypt)
	if err != nil {
		utils.RestError(w, err, err.Error(), http.StatusInternalServerError)
		return
	}

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
		utils.RestError(w, err, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}

	newAccount.ID = ObjectID
	newAccount.Prn = "prn:::accounts:/" + newAccount.ID.Hex()
	newAccount.Challenge = utils.GenerateChallenge()
	newAccount.TimeCreated = time.Now()
	newAccount.Type = accounts.AccountTypeUser // XXX: need org approach too
	newAccount.TimeModified = newAccount.TimeCreated

	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	ctx, cancel = context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": newAccount.ID},
		bson.M{"$set": newAccount.Account},
		updateOptions,
	)
	if err != nil {
		utils.RestError(w, err, err.Error(), http.StatusInternalServerError)
		return
	}

	urlPrefix := utils.GetEnv(utils.EnvPantahubScheme) + "://" + utils.GetEnv(utils.EnvPantahubWWWHost)
	if utils.GetEnv(utils.EnvPantahubPort) != "" {
		urlPrefix += ":"
		urlPrefix += utils.GetEnv(utils.EnvPantahubPort)
	}

	utils.SendVerification(newAccount.Email, newAccount.Nick, newAccount.ID.Hex(), newAccount.Challenge, urlPrefix)

	newAccount.Password = ""
	newAccount.Challenge = ""
	w.WriteJson(newAccount)
}

// handleGetProfile Get user profile
// @Summary Get user profile
// @Description Get user profile
// @Accept  json
// @Produce  json
// @Tags auth
// @Security ApiKeyAuth
// @Param id path string true "ID|Nick|PRN"
// @Success 200 {object} accounts.Account
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 404 {object} utils.RError "Account not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /auth [get]
func (a *App) handleGetProfile(w rest.ResponseWriter, r *rest.Request) {
	jwtClaims := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)

	accountPrn := jwtClaims["prn"].(string)

	if accountPrn == "" {
		utils.RestErrorWrapper(w, "Not logged in", http.StatusPreconditionFailed)
		return
	}

	var account accounts.Account
	var ok bool

	if account, ok = accountsdata.DefaultAccounts[accountPrn]; !ok {
		col := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		err := col.FindOne(ctx, bson.M{"prn": accountPrn}).Decode(&account)
		// always unset credentials so we dont end up sending them out
		account.Password = ""
		account.PasswordBcrypt = ""
		account.PasswordScrypt = ""
		account.Challenge = ""

		if err != nil {
			switch err.(type) {
			default:
				utils.RestErrorWrapper(w, "Account "+err.Error(), http.StatusInternalServerError)
				return
			}
		}
	}

	w.WriteJson(account)
}

// handleVerify Verify account payload
// @Summary Verify account payload
// @Description Verify account payload
// @Accept  json
// @Produce  json
// @Tags auth
// @Success 200 {object} accounts.Account
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 404 {object} utils.RError "Account not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /auth/verify [get]
func (a *App) handleVerify(w rest.ResponseWriter, r *rest.Request) {

	newAccount := accounts.Account{}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

	if collection == nil {
		utils.RestErrorWrapper(w, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	r.ParseForm()
	putID := r.FormValue("id")

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	ObjectID, err := primitive.ObjectIDFromHex(putID)
	if err != nil {
		utils.RestErrorWrapper(w, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
		return
	}
	err = collection.FindOne(ctx,
		bson.M{
			"_id": ObjectID,
		}).
		Decode(&newAccount)
	if err != nil {
		utils.RestErrorWrapper(w, "Not Accessible Resource Id", http.StatusForbidden)
		return
	}

	challenge := newAccount.Challenge
	challengeVal := r.FormValue("challenge")

	/* in case someone claims the device like this, update owner */
	if len(challenge) > 0 {
		if challenge == challengeVal {
			newAccount.Challenge = ""
		} else {
			utils.RestErrorWrapper(w, "Invalid Challenge (wrong, used or never existed)", http.StatusPreconditionFailed)
			return
		}
	} else {
		utils.RestErrorWrapper(w, "Invalid Challenge (wrong, used or never existed)", http.StatusPreconditionFailed)
		return
	}

	newAccount.TimeModified = time.Now()
	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": newAccount.ID},
		bson.M{"$set": newAccount},
		updateOptions,
	)
	if err != nil {
		utils.RestErrorWrapper(w, "Error on Updating", http.StatusInternalServerError)
		return
	}

	urlPrefix := utils.GetEnv(utils.EnvPantahubScheme) + "://" + utils.GetEnv(utils.EnvPantahubWWWHost)
	if utils.GetEnv(utils.EnvPantahubPort) != "" {
		urlPrefix += ":"
		urlPrefix += utils.GetEnv(utils.EnvPantahubPort)
	}

	utils.SendWelcome(newAccount.Email, newAccount.Nick, urlPrefix)

	// always wipe secrets before sending over wire
	newAccount.Password = ""
	newAccount.Challenge = ""
	w.WriteJson(newAccount)
}

// handlePasswordReset gets the recovery token and validate it in order to overwrite the user password
// @Summary send email with token to user in order to reset password to given user
// @Description send email with token to user in order to reset password to given user
// @Accept  json
// @Produce  json
// @Tags auth
// @Param body body authmodels.PasswordReset true "New password payload"
// @Success 200 {object} accounts.Account
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 404 {object} utils.RError "Account not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /auth/password [post]
func (a *App) handlePasswordReset(writer rest.ResponseWriter, r *rest.Request) {
	data := authmodels.PasswordReset{}

	r.DecodeJsonPayload(&data)

	if data.Token == "" {
		utils.RestError(writer, nil, exchangeTokenRequiredErr, http.StatusBadRequest)
		return
	}

	if data.Password == "" {
		utils.RestError(writer, nil, passwordIsNeededErr, http.StatusBadRequest)
		return
	}

	token, err := jwtgo.ParseWithClaims(data.Token, &authmodels.ResetPasswordClaims{}, func(token *jwtgo.Token) (interface{}, error) {
		return a.jwtMiddleware.Pub, nil
	})
	if err != nil {
		utils.RestError(writer, err, tokenInvalidOrExpiredErr, http.StatusInternalServerError)
		return
	}

	claims := token.Claims.(*authmodels.ResetPasswordClaims)
	err = claims.Valid()
	if err != nil {
		utils.RestError(writer, err, tokenInvalidOrExpiredErr, http.StatusInternalServerError)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	if collection == nil {
		utils.RestError(writer, nil, dbConnectionErr, http.StatusInternalServerError)
		return
	}

	filter := bson.M{
		"email":   claims.Email,
		"garbage": bson.M{"$ne": true},
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
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
	if err != nil {
		utils.RestError(writer, err, err.Error(), http.StatusInternalServerError)
		return
	}
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
			"challenge":       "",
			"time-modified":   time.Now(),
		},
	}

	updateOptions := options.Update()
	ctx, cancel = context.WithTimeout(r.Context(), 10*time.Second)
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

// handlePasswordRecovery send email with token to user in order to reset password to given user
// @Summary send email with token to user in order to reset password to given user
// @Description send email with token to user in order to reset password to given user
// @Accept  json
// @Produce  json
// @Tags auth
// @Param body body authmodels.PasswordResetRequest true "Account recovery payload"
// @Success 200 {object} accounts.Account
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 404 {object} utils.RError "Account not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /auth/recover [post]
func (a *App) handlePasswordRecovery(writer rest.ResponseWriter, r *rest.Request) {
	data := authmodels.PasswordResetRequest{}

	r.DecodeJsonPayload(&data)

	if data.Email == "" {
		utils.RestError(writer, nil, emailRequiredForPasswordErr, http.StatusPreconditionFailed)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	if collection == nil {
		utils.RestError(writer, nil, dbConnectionErr, http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
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

	restorePasswordTTL, err := strconv.Atoi(utils.GetEnv(utils.EnvPantahubRecoverJWTTimeoutMinutes))
	if err != nil {
		utils.RestError(writer, err, err.Error(), http.StatusInternalServerError)
	}

	claims := authmodels.ResetPasswordClaims{
		account.Email,
		account.TimeModified,
		jwtgo.StandardClaims{
			ExpiresAt: time.Now().UTC().Add(time.Duration(restorePasswordTTL) * restorePasswordTTLUnit).Unix(),
		},
	}

	token := jwtgo.NewWithClaims(jwtgo.GetSigningMethod(a.jwtMiddleware.SigningAlgorithm), claims)

	tokenString, err := token.SignedString(a.jwtMiddleware.Key)
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

// handlePostToken can be used by services to swap an accessCode to a long living accessToken.
// Payload is of type application/json and type TokenRequest
// note that tokenhandler is supposed to be called authenticated by service that wants the access
// token to be issued on his behalf
// @Summary Get user profile
// @Description Get user profile
// @Accept  json
// @Produce  json
// @Tags auth
// @Security ApiKeyAuth
// @Param id path string true "ID|Nick|PRN"
// @Success 200 {object} accounts.Account
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 404 {object} utils.RError "Account not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /auth/token [post]
func (a *App) handlePostToken(writer rest.ResponseWriter, r *rest.Request) {
	tokenRequest := authmodels.TokenRequest{}
	err := r.DecodeJsonPayload(&tokenRequest)
	if err != nil {
		utils.RestErrorWrapper(writer, "Failed to decode token Request", http.StatusBadRequest)
		return
	}

	// this is the claim of the service authenticating itself
	caller := r.Env["JWT_PAYLOAD"].(jwtgo.MapClaims)["prn"].(string)

	log.Println("Requesting code " + tokenRequest.Code)
	// we parse the accessCode to see if we can swap it out.
	tok, err := jwtgo.Parse(tokenRequest.Code, func(token *jwtgo.Token) (interface{}, error) {
		return a.jwtMiddleware.Pub, nil
	})

	if err != nil {
		log.Println("ERROR: Failed parsing the access Code " + err.Error())
		utils.RestErrorWrapper(writer, "Failed parsing the access Code", http.StatusUnauthorized)
		return
	}

	err = tok.Claims.Valid()
	if err != nil {
		log.Println("ERROR: Failed validating the access Code claims: " + err.Error())
		utils.RestErrorWrapper(writer, "Failed validating the access Code claims", http.StatusUnauthorized)
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
		utils.RestErrorWrapper(writer, "invalid service ("+service+" != "+caller+") tries to swap an accesscode", http.StatusUnauthorized)
		return
	}

	token := jwtgo.New(jwtgo.GetSigningMethod(a.jwtMiddleware.SigningAlgorithm))
	tokenClaims := token.Claims.(jwtgo.MapClaims)

	// lets get the standard payload for a user and modify it so its a service accesstoken
	if a.jwtMiddleware.PayloadFunc != nil {
		for key, value := range a.jwtMiddleware.PayloadFunc(user) {
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

	tokenString, err := token.SignedString(a.jwtMiddleware.Key)

	if err != nil {
		log.Println("WARNING: invalid service (" + service + " != " + caller + ") tries to swap an accesscode")
		utils.RestErrorWrapper(writer, "invalid service ("+service+" != "+caller+") tries to swap an accesscode", http.StatusUnauthorized)
		return
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_oauth_accesstokens")

	if collection == nil {
		utils.RestErrorWrapper(writer, "Error with Database connectivity", http.StatusInternalServerError)
		return
	}

	tokenStore := authmodels.TokenStore{
		ID:      tokenClaims["token_id"].(primitive.ObjectID),
		Client:  service,
		Owner:   user,
		Comment: tokenRequest.Comment,
		Claims:  tokenClaims,
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	_, err = collection.InsertOne(ctx, &tokenStore)
	if err != nil {
		utils.RestErrorWrapper(writer, "Error storing issued token in DB", http.StatusInternalServerError)
		return
	}

	tokenResult := authmodels.TokenResponse{
		Token:     tokenString,
		TokenType: "bearer",
		Scopes:    scopes,
	}

	writer.WriteJson(tokenResult)
}
