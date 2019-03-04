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
package base

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"time"

	"gopkg.in/mgo.v2/bson"

	jwt "github.com/fundapps/go-json-rest-middleware-jwt"
	"github.com/rs/cors"
	"gitlab.com/pantacor/pantahub-base/auth"
	"gitlab.com/pantacor/pantahub-base/dash"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/healthz"
	"gitlab.com/pantacor/pantahub-base/logs"
	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/plog"
	"gitlab.com/pantacor/pantahub-base/subscriptions"
	"gitlab.com/pantacor/pantahub-base/trails"
	"gitlab.com/pantacor/pantahub-base/utils"
)

type FileUploadServer struct {
	fileServer http.Handler
	directory  string
}

func falseAuthenticator(userId string, password string) bool {
	return false
}

func (d FileUploadServer) OpenForWrite(name string) (*os.File, error) {
	fpath, err := utils.MakeLocalS3PathForName(name)
	if err != nil {
		return nil, err
	}

	f, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE, 0644)

	if err != nil {
		return nil, err
	}
	return f, nil
}

func (f FileUploadServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	dirName := filepath.Dir(r.URL.Path)
	fileBase := filepath.Base(r.URL.Path)

	tok, err := objects.NewFromValidToken(fileBase)

	if err != nil {
		log.Println("Invalid local-s3 request (" + fileBase + "): " + err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	objClaims := tok.Token.Claims.(*objects.ObjectAccessClaims)
	storageId := objClaims.Audience
	p, _ := url.Parse(path.Join(dirName, storageId))
	r.URL = r.URL.ResolveReference(p)

	if r.Method == "GET" {
		if objClaims.Method != http.MethodGet {
			log.Println("Invalid objClaims Method; not GET (" + objClaims.Method + ")")
			w.WriteHeader(http.StatusForbidden)
			return
		}

		w.Header().Add("Content-Disposition", "attachment; filename=\""+objClaims.DispositionName+"\"")
		f.fileServer.ServeHTTP(w, r)
		return
	}

	if objClaims.Method != http.MethodPut {
		log.Println("Invalid objClaims Method; not PUT (" + objClaims.Method + ")")
		w.WriteHeader(http.StatusForbidden)
		return
	}

	if objClaims.Sha == "" {
		log.Println("Invalid objClaims Method; no Sha included")
		w.WriteHeader(http.StatusBadRequest)
	}

	uniqueID := bson.NewObjectId().Hex()

	file, err := f.OpenForWrite(storageId + "." + uniqueID)
	if err != nil {
		log.Println("ERROR: opening file for write: " + err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	finalName, err := utils.MakeLocalS3PathForName(storageId)
	if err != nil {
		log.Println("ERROR: creating filepath for write: " + err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	defer file.Close()
	defer r.Body.Close()

	hasher := sha256.New()
	fw := io.MultiWriter(file, hasher)
	var sha []byte
	var shaS string

	written, err := io.CopyN(fw, r.Body, objClaims.Size)

	if err != nil {
		log.Println("ERROR: error syncing file upload to disk: " + err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		goto fail
	}
	if written != objClaims.Size {
		log.Println("WARNING: file upload size mismatch with claim")
		w.WriteHeader(http.StatusBadRequest)
		goto fail
	}

	sha = hasher.Sum(nil)
	shaS = hex.EncodeToString(sha)

	if shaS != objClaims.Sha {
		log.Println("WARNING: file upload sha mismatch with claim: " + shaS + " != " + objClaims.Sha)
		w.WriteHeader(http.StatusBadRequest)
		goto fail
	}
	file.Close()

	err = os.Rename(file.Name(), finalName)

	if err != nil {
		log.Println("ERROR: failed to rename successfully and validated file after upload: " + err.Error())
		goto fail
	}

	return
fail:
	file.Close()
	err = os.Remove(file.Name())
	if err != nil {
		log.Println("ERROR: created file cannot be deleted: " + err.Error())
	}
}

var fserver *FileUploadServer

func GetFileUploadServer() *FileUploadServer {
	return fserver
}

func DoInit() {

	phAuth := utils.GetEnv(utils.ENV_PANTAHUB_AUTH)
	jwtSecret := utils.GetEnv(utils.ENV_PANTAHUB_JWT_AUTH_SECRET)

	session, _ := utils.GetMongoSession()

	adminUsers := utils.GetSubscriptionAdmins()
	subService := subscriptions.NewService(session, utils.Prn("prn::subscriptions:"),
		adminUsers, subscriptions.SubscriptionProperties)

	{
		app := auth.New(&jwt.JWTMiddleware{
			Key:        []byte(jwtSecret),
			Realm:      "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Timeout:    time.Minute * 60,
			MaxRefresh: time.Hour * 24,
		}, session)
		http.Handle("/auth/", http.StripPrefix("/auth", app.Api.MakeHandler()))
	}
	{
		app := objects.New(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, subService, session)
		http.Handle("/objects/", http.StripPrefix("/objects", app.Api.MakeHandler()))
	}
	{
		app := devices.New(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, session)
		http.Handle("/devices/", http.StripPrefix("/devices", app.Api.MakeHandler()))
	}
	{
		app := trails.New(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, session)
		http.Handle("/trails/", http.StripPrefix("/trails", app.Api.MakeHandler()))
	}
	{
		app := plog.New(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, session)
		http.Handle("/plog/", http.StripPrefix("/plog", app.Api.MakeHandler()))
	}
	{
		app := logs.New(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, session)
		http.Handle("/logs/", http.StripPrefix("/logs", app.Api.MakeHandler()))
	}

	{
		app := healthz.New(session)
		http.Handle("/healthz/", http.StripPrefix("/healthz", app.Api.MakeHandler()))
	}
	{
		app := dash.New(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, subService, session)
		http.Handle("/dash/", http.StripPrefix("/dash", app.Api.MakeHandler()))
	}
	{
		app := subscriptions.NewResty(&jwt.JWTMiddleware{
			Key:           []byte(jwtSecret),
			Realm:         "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Authenticator: falseAuthenticator,
		}, subService, session)
		http.Handle("/subscriptions/", http.StripPrefix("/subscriptions", app.MakeHandler()))
	}

	log.Println("S3 Path: " + objects.PantahubS3Path())
	fservermux := &FileUploadServer{fileServer: http.FileServer(http.Dir(objects.PantahubS3Path())), directory: objects.PantahubS3Path()}
	// default cors - allow GET and POST from all origins
	fserver := cors.AllowAll().Handler(fservermux)

	http.Handle("/local-s3/", http.StripPrefix("/local-s3", fserver))
}
