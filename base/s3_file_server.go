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
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"path/filepath"

	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/s3"
	"gitlab.com/pantacor/pantahub-base/utils"
)

type S3FileServer struct {
	s3 s3.S3
}

func (s *S3FileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
		w.Header().Add("Content-Length", fmt.Sprintf("%d", objClaims.Size))

		downloadURL, err := s.s3.DownloadURL(finalName)
		if err != nil {
			log.Printf("ERROR: getting download url, %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		s3resp, err := http.Get(downloadURL)
		if err != nil {
			log.Printf("ERROR: requesting download file, %v\n", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if s3resp.StatusCode != http.StatusOK {
			log.Printf("ERROR: unexpected response from s3 server, status code %v\n", s3resp.StatusCode)
			w.WriteHeader(s3resp.StatusCode)
			return
		}

		io.CopyN(w, s3resp.Body, objClaims.Size)
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

	tempName := finalName + "_part"
	preSignedURL, err := s.s3.UploadURL(tempName)
	if err != nil {
		log.Printf("ERROR: failed to generate upload url, %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// avoid body close for later sha256 calc
	hasher := sha256.New()
	s3Body := io.TeeReader(r.Body, hasher)

	s3req, err := http.NewRequest(http.MethodPut, preSignedURL, s3Body)
	if err != nil {
		log.Printf("ERROR: failed to generate s3 request, %v\n", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	s3resp, err := http.DefaultClient.Do(s3req)
	if err != nil {
		defer s.s3.Delete(tempName)
		log.Printf("ERROR: failed to upload to %s\n", preSignedURL)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	s.s3.Rename(tempName, finalName)
	defer s3resp.Body.Close()

	if s3resp.StatusCode != http.StatusOK {
		log.Println("ERROR: unexpected response from remote S3 server")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	sha := hasher.Sum(nil)
	shaS := hex.EncodeToString(sha)

	if shaS != objClaims.Sha {
		log.Println("WARNING: file upload sha mismatch with claim: " + shaS + " != " + objClaims.Sha)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusOK)
	return
}

func NewS3FileServer() *S3FileServer {
	connParams := s3.S3ConnectionParameters{
		AccessKey: utils.GetEnv(utils.ENV_PANTAHUB_S3_ACCESS_KEY_ID),
		SecretKey: utils.GetEnv(utils.ENV_PANTAHUB_S3_SECRET_ACCESS_KEY),
		Region:    utils.GetEnv(utils.ENV_PANTAHUB_S3_REGION),
		Bucket:    utils.GetEnv(utils.ENV_PANTAHUB_S3_BUCKET),
		Endpoint:  utils.GetEnv(utils.ENV_PANTAHUB_S3_ENDPOINT),
	}

	return &S3FileServer{
		s3: s3.NewS3(connParams),
	}
}
