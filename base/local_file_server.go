//
// Copyright 2019  Pantacor Ltd.
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

	"gopkg.in/mgo.v2/bson"

	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/utils"
)

type LocalFileServer struct {
	fileServer http.Handler
	directory  string
}

func falseAuthenticator(userId string, password string) bool {
	return false
}

func (d LocalFileServer) openForWrite(name string) (*os.File, error) {
	fpath, err := utils.MakeLocalS3PathForName(name)
	if err != nil {
		return nil, err
	}

	dir, _ := filepath.Split(fpath)
	if _, err = os.Stat(dir); os.IsNotExist(err) {
		os.MkdirAll(dir, os.ModeDir)
	}

	f, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE, 0644)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (f LocalFileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	dirName := filepath.Dir(r.URL.Path)
	fileBase := filepath.Base(r.URL.Path)

	tok, err := objects.NewFromValidToken(fileBase)
	if err != nil {
		log.Println("Invalid local-s3 request (" + fileBase + "): " + err.Error())
		w.WriteHeader(http.StatusForbidden)
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

	file, err := f.openForWrite(storageId + "." + uniqueID)
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

var fserver *LocalFileServer

func GetLocalFileServer() *LocalFileServer {
	return fserver
}
