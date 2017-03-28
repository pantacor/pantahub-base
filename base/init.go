//
// Copyright 2016  Alexander Sack <asac129@gmail.com>
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

	session, _ := utils.GetMongoSession()

	{
		app := auth.New(&jwt.JWTMiddleware{
			Key:        []byte("secret key"),
			Realm:      "pantahub services",
			Timeout:    time.Minute * 60,
			MaxRefresh: time.Hour * 24,
		}, session)
		http.Handle("/auth/", http.StripPrefix("/auth", app.Api.MakeHandler()))
	}
	{
		app := objects.New(&jwt.JWTMiddleware{
			Key:   []byte("secret key"),
			Realm: "pantahub services",
		}, session)
		http.Handle("/objects/", http.StripPrefix("/api/objects", app.Api.MakeHandler()))
	}
	{
		app := devices.New(&jwt.JWTMiddleware{
			Key:   []byte("secret key"),
			Realm: "pantahub services",
		}, session)
		http.Handle("/devices/", http.StripPrefix("/api/devices", app.Api.MakeHandler()))
	}
	{
		app := trails.New(&jwt.JWTMiddleware{
			Key:   []byte("secret key"),
			Realm: "pantahub services",
		}, session)
		http.Handle("/trails/", http.StripPrefix("/trails", app.Api.MakeHandler()))
	}

	{
		app := plog.New(&jwt.JWTMiddleware{
			Key:   []byte("secret key"),
			Realm: "pantahub services",
		}, session)
		http.Handle("/plog/", http.StripPrefix("/plog", app.Api.MakeHandler()))
	}

	if !objects.PantahubS3Production() {
		fmt.Println("S3 Development Path: " + objects.PantahubS3Path())
		fserver := FileUploadServer{fileServer: http.FileServer(http.Dir(objects.PantahubS3Path())), directory: objects.PantahubS3Path()}
		http.Handle("/local-s3/", http.StripPrefix("/local-s3", &fserver))
	}

}
