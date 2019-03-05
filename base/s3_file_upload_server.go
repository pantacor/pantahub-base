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
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"

	"github.com/aws/aws-sdk-go/aws"
	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/s3"
	"gitlab.com/pantacor/pantahub-base/utils"
)

type S3FileUploadServer struct {
	s3 s3.S3
}

func (s *S3FileUploadServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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

	finalName, err := utils.MakeLocalS3PathForName(storageId)
	if err != nil {
		log.Println("ERROR: creating filepath for write: " + err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if r.Method == "GET" {
		if objClaims.Method != http.MethodGet {
			log.Println("Invalid objClaims Method; not GET (" + objClaims.Method + ")")
			w.WriteHeader(http.StatusForbidden)
			return
		}

		w.Header().Add("Content-Disposition", "attachment; filename=\""+objClaims.DispositionName+"\"")

		writeAt := aws.NewWriteAtBuffer([]byte{})
		err := s.s3.Download(finalName, writeAt)
		if err != nil {
			log.Printf("ERROR: downloading file, %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		w.Write(writeAt.Bytes())
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
		return
	}

	defer r.Body.Close()

	hasher := sha256.New()
	tmpFile, err := ioutil.TempFile(os.TempDir(), "pantahub_s3_upload_*")
	if err != nil {
		log.Println("ERROR: error creating temporarly file: " + err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	defer os.Remove(tmpFile.Name())

	fw := io.MultiWriter(tmpFile, hasher)

	written, err := io.CopyN(fw, r.Body, objClaims.Size)
	if err != nil {
		log.Println("ERROR: error syncing file upload to disk: " + err.Error())
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if written != objClaims.Size {
		log.Println("WARNING: file upload size mismatch with claim")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	sha := hasher.Sum(nil)
	shaS := hex.EncodeToString(sha)

	if shaS != objClaims.Sha {
		log.Println("WARNING: file upload sha mismatch with claim: " + shaS + " != " + objClaims.Sha)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	err = s.s3.Upload(finalName, tmpFile)
	if err != nil {
		log.Printf("ERROR: failed to upload to remote S3 server, %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	return
}

func NewS3FileUploadServer() *S3FileUploadServer {
	connParams := s3.S3ConnectionParameters{
		AccessKey: utils.GetEnv(utils.ENV_PANTAHUB_S3_ACCESS_KEY_ID),
		SecretKey: utils.GetEnv(utils.ENV_PANTAHUB_S3_SECRET_ACCESS_KEY),
		Region:    utils.GetEnv(utils.ENV_PANTAHUB_S3_REGION),
		Bucket:    utils.GetEnv(utils.ENV_PANTAHUB_S3_BUCKET),
		Endpoint:  utils.GetEnv(utils.ENV_PANTAHUB_S3_ENDPOINT),
	}

	return &S3FileUploadServer{
		s3: s3.NewS3(connParams),
	}
}
