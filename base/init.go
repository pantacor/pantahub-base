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
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"pantahub-base/auth"
	"pantahub-base/devices"
	"pantahub-base/objects"
	"pantahub-base/plog"
	"pantahub-base/trails"
	"pantahub-base/utils"

	"github.com/StephanDollberg/go-json-rest-middleware-jwt"
)

type FileUploadServer struct {
	fileServer http.Handler
	directory  string
}

func (d FileUploadServer) OpenForWrite(name string) (*os.File, error) {
	if filepath.Separator != '/' && strings.ContainsRune(name, filepath.Separator) ||
		strings.Contains(name, "\x00") {
		return nil, errors.New("http: invalid character in file path")
	}
	dir := d.directory
	if dir == "" {
		dir = "."
	}

	fpath := filepath.Join(dir, filepath.FromSlash(path.Clean("/"+name)))

	f, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE, 0644)

	if err != nil {
		return nil, err
	}
	return f, nil
}

func (f FileUploadServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {

	if r.Method == "GET" {
		f.fileServer.ServeHTTP(w, r)
		return
	}
	file, err := f.OpenForWrite(r.URL.Path)

	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	defer file.Close()
	defer r.Body.Close()

	io.Copy(file, r.Body)
}

func DoInit() {

	phAuth := utils.GetEnv(utils.ENV_PANTAHUB_AUTH)

	session, _ := utils.GetMongoSession()

	{
		app := auth.New(&jwt.JWTMiddleware{
			Key:        []byte("secret key"),
			Realm:      "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
			Timeout:    time.Minute * 60,
			MaxRefresh: time.Hour * 24,
		}, session)
		http.Handle("/auth/", http.StripPrefix("/auth", app.Api.MakeHandler()))
	}
	{
		app := objects.New(&jwt.JWTMiddleware{
			Key:   []byte("secret key"),
			Realm: "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
		}, session)
		http.Handle("/objects/", http.StripPrefix("/objects", app.Api.MakeHandler()))
	}
	{
		app := devices.New(&jwt.JWTMiddleware{
			Key:   []byte("secret key"),
			Realm: "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
		}, session)
		http.Handle("/devices/", http.StripPrefix("/devices", app.Api.MakeHandler()))
	}
	{
		app := trails.New(&jwt.JWTMiddleware{
			Key:   []byte("secret key"),
			Realm: "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
		}, session)
		http.Handle("/trails/", http.StripPrefix("/trails", app.Api.MakeHandler()))
	}
	{
		app := plog.New(&jwt.JWTMiddleware{
			Key:   []byte("secret key"),
			Realm: "\"pantahub services\", ph-aeps=\"" + phAuth + "\"",
		}, session)
		http.Handle("/plog/", http.StripPrefix("/plog", app.Api.MakeHandler()))
	}

	if !objects.PantahubS3Production() {
		fmt.Println("S3 Development Path: " + objects.PantahubS3Path())
		fserver := FileUploadServer{fileServer: http.FileServer(http.Dir(objects.PantahubS3Path())), directory: objects.PantahubS3Path()}
		http.Handle("/local-s3/", http.StripPrefix("/local-s3", &fserver))
	}

}
