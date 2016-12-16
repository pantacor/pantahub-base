//
// Copyright 2016  Alexander Sack <asac129@gmail.com>
//
package main

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"pantahub-base/auth"
	"pantahub-base/devices"
	"pantahub-base/objects"
	"pantahub-base/trails"

	"github.com/StephanDollberg/go-json-rest-middleware-jwt"

	"labix.org/v2/mgo"
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

func main() {

	// XXX: make mongo host configurable through env
	mongoHost := os.Getenv("MONGO_HOST")
	if mongoHost == "" {
		mongoHost = "localhost"
	}

	mongoPort := os.Getenv("MONGO_PORT")
	if mongoPort == "" {
		mongoPort = "27017"
	}

	session, err := mgo.Dial(mongoHost + ":" + mongoPort)

	if err != nil {
		panic(err)
	}

	{
		app := auth.New(&jwt.JWTMiddleware{
			Key:        []byte("secret key"),
			Realm:      "pantahub services",
			Timeout:    time.Minute * 60,
			MaxRefresh: time.Hour * 24,
		}, session)
		http.Handle("/api/auth/", http.StripPrefix("/api/auth", app.Api.MakeHandler()))
	}
	{
		app := objects.New(&jwt.JWTMiddleware{
			Key:   []byte("secret key"),
			Realm: "pantahub services",
		}, session)
		http.Handle("/api/objects/", http.StripPrefix("/api/objects", app.Api.MakeHandler()))
	}
	{
		app := devices.New(&jwt.JWTMiddleware{
			Key:   []byte("secret key"),
			Realm: "pantahub services",
		}, session)
		http.Handle("/api/devices/", http.StripPrefix("/api/devices", app.Api.MakeHandler()))
	}
	{
		app := trails.New(&jwt.JWTMiddleware{
			Key:   []byte("secret key"),
			Realm: "pantahub services",
		}, session)
		http.Handle("/api/trails/", http.StripPrefix("/api/trails", app.Api.MakeHandler()))
	}

	if !objects.PantahubS3Production() {
		fmt.Println("S3 Development Path: " + objects.PantahubS3Path())
		fserver := FileUploadServer{fileServer: http.FileServer(http.Dir(objects.PantahubS3Path())), directory: objects.PantahubS3Path()}
		http.Handle("/local-s3/", http.StripPrefix("/local-s3", &fserver))
	}

	go func() {
		log.Fatal(http.ListenAndServeTLS(":12366", "localhost.cert.pem", "localhost.key.pem", nil))
	}()

	ifaces, _ := net.Interfaces()
	for _, i := range ifaces {
		addrs, _ := i.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			log.Printf("Serving @ https://" + ip.String() + ":12366/api/\n")
			log.Printf("Serving @ http://" + ip.String() + ":12365/api/\n")
		}
	}
	log.Fatal(http.ListenAndServe(":12365", nil))
}
