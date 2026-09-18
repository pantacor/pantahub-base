// Copyright (c) 2017-2026 Pantacor Ltd.
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
	"errors"
	"gitlab.com/pantacor/pantahub-base/auth/authservices"
	"log"
	"net/http"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"

	jwtgo "github.com/golang-jwt/jwt/v5"
	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/accounts/accountsdata"
	"gitlab.com/pantacor/pantahub-base/auth/authmodels"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
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
func handleAuthStatus(c *echo.Context) error {
	jwtClaims := c.Get(echoutil.KeyJWTPayload)
	return echoutil.WriteJSON(c, http.StatusOK, jwtClaims)
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
func (a *App) handleGetAccounts(c *echo.Context) error {
	var err error
	var cur *mongo.Cursor

	authInfo := echoutil.AuthInfo(c)
	_ = c.Request().ParseForm()
	asAdminMode := c.Request().FormValue("asadmin")

	if asAdminMode != "" && authInfo.Roles != "admin" {
		return echoutil.RestError(c, nil, "user has no admin role", http.StatusForbidden)
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

	if collection == nil {
		return echoutil.RestError(c, nil, "Error with Database connectivity", http.StatusInternalServerError)
	}

	resultSet := make([]accounts.AccountPublic, 0)
	findOptions := options.Find()
	findOptions.SetNoCursorTimeout(true)
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
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
		return echoutil.RestError(c, err, "Error on fetching accounts.", http.StatusInternalServerError)
	}
	defer cur.Close(ctx)

	for cur.Next(ctx) {
		result := accounts.AccountPublic{}
		err := cur.Decode(&result)
		if err != nil {
			return echoutil.RestError(c, err, "Cursor Decode Error", http.StatusInternalServerError)
		}
		resultSet = append(resultSet, result)
	}

	return echoutil.WriteJSON(c, http.StatusOK, &resultSet)
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
func (a *App) handlePostSession(c *echo.Context) error {

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
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	_, err := collection.InsertOne(
		ctx,
		sessionAccount,
		&opts,
	)
	if err != nil {
		return echoutil.RestError(c, err, err.Error(), http.StatusInternalServerError)
	}

	token := jwtgo.New(jwtgo.GetSigningMethod(a.jwtConfig.SigningAlgorithm))
	tokenClaims := token.Claims.(jwtgo.MapClaims)

	// lets get the standard payload for a user and modify it so its a service accesstoken
	if a.jwtConfig.PayloadFunc != nil {
		for key, value := range a.jwtConfig.PayloadFunc(sessionAccount.Prn) {
			tokenClaims[key] = value
		}
	}

	tokenClaims["id"] = sessionAccount.Nick
	tokenClaims["exp"] = time.Now().Add(a.jwtConfig.Timeout).Unix()
	if a.jwtConfig.MaxRefresh != 0 {
		tokenClaims["orig_iat"] = time.Now().Unix()
	}

	tokenString, err := token.SignedString(a.jwtConfig.Key)

	if err != nil {
		return echoutil.RestErrorWrapper(c, "error creating one time token "+err.Error(), http.StatusInternalServerError)
	}

	sessionAccount.Password = ""
	sessionAccount.Challenge = ""

	return echoutil.WriteJSON(c, http.StatusOK, bson.M{"token": tokenString})
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
func (a *App) handlePostAccount(c *echo.Context) error {
	newAccount := authmodels.AccountCreationPayload{}

	if err := echoutil.DecodeJsonPayload(c, &newAccount); err != nil {
		return echoutil.RestErrorWrapper(c, "Error decoding json payload: "+err.Error(), http.StatusBadRequest)
	}

	if utils.GetEnv(utils.EnvPantahubDisableSignup) == "true" {
		return echoutil.RestError(c, nil, "User signup is currently disabled", http.StatusForbidden)
	}

	// if encrypted account data exist decryted and continue with validation
	if newAccount.EncryptedAccount != "" {
		err := utils.ParseJWE(newAccount.EncryptedAccount, &newAccount.Account)
		if err != nil {
			return echoutil.RestError(c, err, err.Error(), http.StatusInternalServerError)
		}
	}

	if newAccount.Email == "" {
		return echoutil.RestError(c, nil, "Accounts must have an email address", http.StatusPreconditionFailed)
	}

	// same allowlist that gates login: refuse the signup up front instead of
	// creating an account (and sending a verification mail) that can never
	// sign in
	if !authservices.IsEmailDomainAllowed(newAccount.Email) {
		return echoutil.RestError(c, nil, "Accounts with this email domain are not allowed", http.StatusForbidden)
	}

	if newAccount.Password == "" {
		return echoutil.RestError(c, nil, "Accounts must have a password set", http.StatusPreconditionFailed)
	}

	if newAccount.Nick == "" {
		return echoutil.RestError(c, nil, "Accounts must have a nick set", http.StatusPreconditionFailed)
	}

	if !utils.IsNick(newAccount.Nick) {
		return echoutil.RestError(c, nil, "Accounts must have a a valid nick", http.StatusPreconditionFailed)
	}

	if !newAccount.ID.IsZero() {
		return echoutil.RestError(c, nil, "Accounts cannot have id before creation", http.StatusPreconditionFailed)
	}

	// Validate if user already exist
	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

	if collection == nil {
		return echoutil.RestError(c, nil, "Error with Database connectivity", http.StatusInternalServerError)
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
		return echoutil.RestErrorUser(c, nil, "Email or Nick already in use", http.StatusPreconditionFailed)
	}

	// if account creation doesn't have captcha encrypt data and send a redirect link to finish the process
	useCaptcha := utils.GetEnv(utils.EnvPantahubUseCaptcha) == "true"
	if newAccount.Captcha == "" && useCaptcha {
		response, err := handleGetEncryptedAccount(&newAccount)
		if err != nil {
			return echoutil.RestError(c, err, err.Error(), http.StatusInternalServerError)
		}
		return echoutil.WriteJSON(c, http.StatusOK, response)
	}

	if useCaptcha {
		validCaptcha, err := utils.VerifyReCaptchaToken(newAccount.Captcha)
		if err != nil {
			return echoutil.RestError(c, err, err.Error(), http.StatusPreconditionFailed)
		}
		if !validCaptcha {
			return echoutil.RestError(c, nil, "Invalid captcha", http.StatusPreconditionFailed)
		}
	}

	passwordBcrypt, err := utils.HashPassword(newAccount.Password, utils.CryptoMethods.BCrypt)
	if err != nil {
		return echoutil.RestError(c, err, err.Error(), http.StatusInternalServerError)
	}

	newAccount.Password = ""
	newAccount.PasswordBcrypt = passwordBcrypt

	mgoid := primitive.NewObjectID()
	ObjectID, err := primitive.ObjectIDFromHex(mgoid.Hex())
	if err != nil {
		return echoutil.RestError(c, err, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
	}

	newAccount.ID = ObjectID
	newAccount.Prn = "prn:::accounts:/" + newAccount.ID.Hex()
	newAccount.Challenge = utils.GenerateChallenge()
	newAccount.TimeCreated = time.Now()
	newAccount.Type = accounts.AccountTypeUser // XXX: need org approach too
	newAccount.TimeModified = newAccount.TimeCreated

	updateOptions := options.Update()
	updateOptions.SetUpsert(true)
	ctx, cancel = context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	_, err = collection.UpdateOne(
		ctx,
		bson.M{"_id": newAccount.ID},
		bson.M{"$set": newAccount.Account},
		updateOptions,
	)
	if err != nil {
		return echoutil.RestError(c, err, err.Error(), http.StatusInternalServerError)
	}

	urlPrefix := utils.GetEnv(utils.EnvPantahubScheme) + "://" + utils.GetEnv(utils.EnvPantahubWWWHost)
	if utils.GetEnv(utils.EnvPantahubPort) != "" {
		urlPrefix += ":"
		urlPrefix += utils.GetEnv(utils.EnvPantahubPort)
	}

	utils.SendVerification(newAccount.Email, newAccount.Nick, newAccount.ID.Hex(), newAccount.Challenge, urlPrefix)

	newAccount.Password = ""
	newAccount.Challenge = ""
	return echoutil.WriteJSON(c, http.StatusOK, newAccount)
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
func (a *App) handleGetProfile(c *echo.Context) error {
	jwtClaims := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)

	accountPrn := jwtClaims["prn"].(string)

	if accountPrn == "" {
		return echoutil.RestErrorWrapper(c, "Not logged in", http.StatusPreconditionFailed)
	}

	var account accounts.Account
	var ok bool

	if account, ok = accountsdata.DefaultAccounts[accountPrn]; !ok {
		col := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
		ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
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
				return echoutil.RestErrorWrapper(c, "Account "+err.Error(), http.StatusInternalServerError)
			}
		}
	}

	return echoutil.WriteJSON(c, http.StatusOK, account)
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
func (a *App) handleVerify(c *echo.Context) error {

	newAccount := accounts.Account{}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")

	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	_ = c.Request().ParseForm()
	putID := c.Request().FormValue("id")

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	ObjectID, err := primitive.ObjectIDFromHex(putID)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Invalid Hex:"+err.Error(), http.StatusInternalServerError)
	}
	err = collection.FindOne(ctx,
		bson.M{
			"_id": ObjectID,
		}).
		Decode(&newAccount)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Not Accessible Resource Id", http.StatusForbidden)
	}

	challenge := newAccount.Challenge
	challengeVal := c.Request().FormValue("challenge")

	/* in case someone claims the device like this, update owner */
	if len(challenge) > 0 {
		if challenge == challengeVal {
			newAccount.Challenge = ""
		} else {
			return echoutil.RestErrorWrapper(c, "Invalid Challenge (wrong, used or never existed)", http.StatusPreconditionFailed)
		}
	} else {
		return echoutil.RestErrorWrapper(c, "Invalid Challenge (wrong, used or never existed)", http.StatusPreconditionFailed)
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
		return echoutil.RestErrorWrapper(c, "Error on Updating", http.StatusInternalServerError)
	}

	urlPrefix := utils.GetEnv(utils.EnvPantahubScheme) + "://" + utils.GetEnv(utils.EnvPantahubWWWHost)
	if utils.GetEnv(utils.EnvPantahubPort) != "" {
		urlPrefix += ":"
		urlPrefix += utils.GetEnv(utils.EnvPantahubPort)
	}

	if err := utils.SendWelcome(newAccount.Email, newAccount.Nick, urlPrefix); err != nil {
		log.Printf("WARNING: sending welcome mail to the new account failed: %v", err)
	}

	// always wipe secrets before sending over wire
	newAccount.Password = ""
	newAccount.Challenge = ""
	return echoutil.WriteJSON(c, http.StatusOK, newAccount)
}

// handlePasswordReset gets the recovery token and validate it in order to overwrite the user password
// @Summary gets the recovery token and validate it in order to overwrite the user password
// @Description gets the recovery token and validate it in order to overwrite the user password
// @Accept  json
// @Produce  json
// @Tags auth
// @Param body body authmodels.PasswordReset true "New password payload"
// @Success 200 {object} accounts.Account
// @Failure 400 {object} utils.RError "Invalid payload"
// @Failure 404 {object} utils.RError "Account not found"
// @Failure 500 {object} utils.RError "Error processing request"
// @Router /auth/password [post]
func (a *App) handlePasswordReset(c *echo.Context) error {
	data := authmodels.PasswordReset{}

	if err := echoutil.DecodeJsonPayload(c, &data); err != nil {
		return echoutil.RestError(c, err, "Error decoding json payload", http.StatusBadRequest)
	}

	if data.Token == "" {
		return echoutil.RestError(c, nil, exchangeTokenRequiredErr, http.StatusBadRequest)
	}

	if data.Password == "" {
		return echoutil.RestError(c, nil, passwordIsNeededErr, http.StatusBadRequest)
	}

	token, err := jwtgo.ParseWithClaims(data.Token, &authmodels.ResetPasswordClaims{}, func(token *jwtgo.Token) (interface{}, error) {
		if token.Method.Alg() != a.jwtConfig.SigningAlgorithm {
			return nil, errors.New("invalid signing algorithm")
		}
		return a.jwtConfig.Pub, nil
	})
	if err != nil {
		return echoutil.RestError(c, err, tokenInvalidOrExpiredErr, http.StatusInternalServerError)
	}

	claims := token.Claims.(*authmodels.ResetPasswordClaims)
	// v5 validates claims during parsing (Claims.Valid was removed).
	if !token.Valid {
		return echoutil.RestError(c, errors.New("invalid token claims"), tokenInvalidOrExpiredErr, http.StatusInternalServerError)
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	if collection == nil {
		return echoutil.RestError(c, nil, dbConnectionErr, http.StatusInternalServerError)
	}

	filter := bson.M{
		"email":   claims.Email,
		"garbage": bson.M{"$ne": true},
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	account := accounts.AccountPublic{}
	err = collection.FindOne(ctx, filter).Decode(&account)
	if err != nil {
		return echoutil.RestError(c, nil, emailNotFoundErr, http.StatusNotFound)
	}

	if !account.TimeModified.Equal(claims.TimeModified) {
		return echoutil.RestError(c, nil, tokenInvalidOrExpiredErr, http.StatusBadRequest)
	}

	passwordBcrypt, err := utils.HashPassword(data.Password, utils.CryptoMethods.BCrypt)
	if err != nil {
		return echoutil.RestError(c, err, err.Error(), http.StatusInternalServerError)
	}
	update := bson.M{
		"$set": bson.M{
			"password":        "",
			"password_bcrypt": passwordBcrypt,
			"password_scrypt": "",
			"challenge":       "",
			"time-modified":   time.Now(),
		},
	}

	updateOptions := options.Update()
	ctx, cancel = context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	_, err = collection.UpdateOne(
		ctx,
		filter,
		update,
		updateOptions,
	)
	if err != nil {
		return echoutil.RestError(c, err, err.Error(), http.StatusInternalServerError)
	}

	return echoutil.WriteJSON(c, http.StatusOK, true)
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
func (a *App) handlePasswordRecovery(c *echo.Context) error {
	if utils.GetEnv(utils.EnvPantahubDisableForgotPassword) == "true" {
		return echoutil.RestError(c, nil, "Recover password feature is disabled", http.StatusForbidden)
	}

	data := authmodels.PasswordResetRequest{}

	if err := echoutil.DecodeJsonPayload(c, &data); err != nil {
		return echoutil.RestError(c, err, "Error decoding json payload", http.StatusBadRequest)
	}

	if data.Email == "" {
		return echoutil.RestError(c, nil, emailRequiredForPasswordErr, http.StatusPreconditionFailed)
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_accounts")
	if collection == nil {
		return echoutil.RestError(c, nil, dbConnectionErr, http.StatusInternalServerError)
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()

	account := accounts.AccountPublic{}
	filter := bson.M{
		"email":   data.Email,
		"garbage": bson.M{"$ne": true},
	}

	err := collection.FindOne(ctx, filter).Decode(&account)
	if err != nil {
		return echoutil.RestError(c, nil, emailNotFoundErr, http.StatusNotFound)
	}

	restorePasswordTTL, err := strconv.Atoi(utils.GetEnv(utils.EnvPantahubRecoverJWTTimeoutMinutes))
	if err != nil {
		return echoutil.RestError(c, err, err.Error(), http.StatusInternalServerError)
	}

	claims := authmodels.ResetPasswordClaims{
		account.Email,
		account.TimeModified,
		jwtgo.RegisteredClaims{
			ExpiresAt: jwtgo.NewNumericDate(time.Now().UTC().Add(time.Duration(restorePasswordTTL) * restorePasswordTTLUnit)),
		},
	}

	token := jwtgo.NewWithClaims(jwtgo.GetSigningMethod(a.jwtConfig.SigningAlgorithm), claims)

	tokenString, err := token.SignedString(a.jwtConfig.Key)
	if err != nil {
		return echoutil.RestError(c, err, tokenCreationErr, http.StatusInternalServerError)
	}

	err = utils.SendResetPasswordEmail(account.Email, account.Nick, tokenString)
	if err != nil {
		return echoutil.RestError(c, err, sendEmailErr, http.StatusInternalServerError)
	}

	return echoutil.WriteJSON(c, http.StatusOK, true)
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
func (a *App) handlePostToken(c *echo.Context) error {
	tokenRequest := authmodels.TokenRequest{}
	err := echoutil.DecodeJsonPayload(c, &tokenRequest)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Failed to decode token Request", http.StatusBadRequest)
	}

	// this is the claim of the service authenticating itself
	caller := c.Get(echoutil.KeyJWTPayload).(jwtgo.MapClaims)["prn"].(string)

	log.Println("Requesting code " + tokenRequest.Code)
	// we parse the accessCode to see if we can swap it out.
	tok, err := jwtgo.Parse(tokenRequest.Code, func(token *jwtgo.Token) (interface{}, error) {
		if token.Method.Alg() != a.jwtConfig.SigningAlgorithm {
			return nil, errors.New("invalid signing algorithm")
		}
		return a.jwtConfig.Pub, nil
	})

	if err != nil {
		log.Println("ERROR: Failed parsing the access Code " + err.Error())
		return echoutil.RestErrorWrapper(c, "Failed parsing the access Code", http.StatusUnauthorized)
	}

	// See the note above on Claims.Valid(): v5 validates during parsing.
	if !tok.Valid {
		log.Println("ERROR: Failed validating the access Code claims")
		return echoutil.RestErrorWrapper(c, "Failed validating the access Code claims", http.StatusUnauthorized)
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
		return echoutil.RestErrorWrapper(c, "invalid service ("+service+" != "+caller+") tries to swap an accesscode", http.StatusUnauthorized)
	}

	token := jwtgo.New(jwtgo.GetSigningMethod(a.jwtConfig.SigningAlgorithm))
	tokenClaims := token.Claims.(jwtgo.MapClaims)

	// lets get the standard payload for a user and modify it so its a service accesstoken
	if a.jwtConfig.PayloadFunc != nil {
		for key, value := range a.jwtConfig.PayloadFunc(user) {
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
	timeoutStr := utils.GetEnv(utils.EnvPantahubAuthorizeJWTTimeoutMinutes)
	authorizeTimeout, err := strconv.Atoi(timeoutStr)
	if err != nil {
		authorizeTimeout = 1920
	}
	tokenClaims["exp"] = time.Now().Add(time.Minute * time.Duration(authorizeTimeout)).Unix()
	tokenClaims["orig_iat"] = time.Now().Unix()

	tokenString, err := token.SignedString(a.jwtConfig.Key)

	if err != nil {
		log.Println("WARNING: invalid service (" + service + " != " + caller + ") tries to swap an accesscode")
		return echoutil.RestErrorWrapper(c, "invalid service ("+service+" != "+caller+") tries to swap an accesscode", http.StatusUnauthorized)
	}

	collection := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_oauth_accesstokens")

	if collection == nil {
		return echoutil.RestErrorWrapper(c, "Error with Database connectivity", http.StatusInternalServerError)
	}

	tokenStore := authmodels.TokenStore{
		ID:      tokenClaims["token_id"].(primitive.ObjectID),
		Client:  service,
		Owner:   user,
		Comment: tokenRequest.Comment,
		Claims:  tokenClaims,
	}

	ctx, cancel := context.WithTimeout(c.Request().Context(), 10*time.Second)
	defer cancel()
	_, err = collection.InsertOne(ctx, &tokenStore)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error storing issued token in DB", http.StatusInternalServerError)
	}

	tokenResult := authmodels.TokenResponse{
		Token:     tokenString,
		TokenType: "bearer",
		Scopes:    scopes,
	}

	return echoutil.WriteJSON(c, http.StatusOK, tokenResult)
}
