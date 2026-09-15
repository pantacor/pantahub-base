//
// Copyright 2026 Pantacor Ltd.
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

// LocalFileServer File server for local
type LocalFileServer struct {
	fileServer http.Handler
	directory  string
}

func falseAuthenticator(userID string, password string) bool {
	return false
}

func (lfs LocalFileServer) openForWrite(name string) (*os.File, error) {
	fpath, err := utils.MakeLocalS3PathForName(name)
	if err != nil {
		return nil, err
	}

	dir, _ := filepath.Split(fpath)
	if _, err = os.Stat(dir); os.IsNotExist(err) {
		// os.ModeDir is the type bit, not a permission: passing it here asked
		// for a directory with mode 0000, which only worked because the
		// process could bypass its own permissions. 0700 is what was meant.
		if err = os.MkdirAll(dir, 0o700); err != nil {
			return nil, err
		}
	}

	// Object blobs are read back by this process alone, so they do not need to
	// be world-readable.
	//
	//#nosec G304 -- fpath comes from MakeLocalS3PathForName, which guarantees
	// the result is inside the storage base (see its containment check).
	f, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	return f, nil
}

func (lfs LocalFileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	dirName := filepath.Dir(r.URL.Path)
	fileBase := filepath.Base(r.URL.Path)

	tok, err := objects.NewFromValidToken(fileBase)
	if err != nil {
		//#nosec G706 -- %q escapes control characters, so the path cannot forge log lines
		log.Printf("Invalid local-s3 request (%q): %v", fileBase, err)
		w.WriteHeader(http.StatusForbidden)
		return
	}

	objClaims := tok.Token.Claims.(*objects.ObjectAccessClaims)
	storageID := objClaims.Audience
	p, _ := url.Parse(path.Join(dirName, storageID))
	r.URL = r.URL.ResolveReference(p)

	if r.Method == "GET" {
		if objClaims.Method != http.MethodGet {
			//#nosec G706 -- from a server-signed claim, and %q escapes it anyway
			log.Printf("Invalid objClaims Method; not GET (%q)", objClaims.Method)
			w.WriteHeader(http.StatusForbidden)
			return
		}

		w.Header().Add("Content-Disposition", "attachment; filename=\""+objClaims.DispositionName+"\"")
		lfs.fileServer.ServeHTTP(w, r)
		return
	}

	if objClaims.Method != http.MethodPut {
		//#nosec G706 -- from a server-signed claim, and %q escapes it anyway
		log.Printf("Invalid objClaims Method; not PUT (%q)", objClaims.Method)
		w.WriteHeader(http.StatusForbidden)
		return
	}

	if objClaims.Sha == "" {
		log.Println("Invalid objClaims Method; no Sha included")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	uniqueID := bson.NewObjectId().Hex()

	file, err := lfs.openForWrite(storageID + "." + uniqueID)
	if err != nil {
		log.Printf("ERROR: opening file for write: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	finalName, err := utils.MakeLocalS3PathForName(storageID)
	if err != nil {
		log.Printf("ERROR: creating filepath for write: %v", err)
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
		log.Printf("ERROR: error syncing file upload to disk: %v", err)
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
		//#nosec G706 -- both values are %q escaped
		log.Printf("WARNING: file upload sha mismatch with claim: %q != %q", shaS, objClaims.Sha)
		w.WriteHeader(http.StatusBadRequest)
		goto fail
	}
	// Closing is where buffered data is finally flushed, so a failure here
	// means the object on disk may be short even though the hash of what was
	// streamed matched. Publishing it with os.Rename would make a corrupt
	// object indistinguishable from a good one.
	if err = file.Close(); err != nil {
		log.Printf("ERROR: closing file after upload: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		goto fail
	}

	err = os.Rename(file.Name(), finalName)

	if err != nil {
		log.Printf("ERROR: failed to rename successfully and validated file after upload: %v", err)
		goto fail
	}

	return
fail:
	// Already failing; this close only releases the descriptor before the file
	// is removed below.
	_ = file.Close()
	err = os.Remove(file.Name())
	if err != nil {
		log.Printf("ERROR: created file cannot be deleted: %v", err)
	}
}

var fserver *LocalFileServer

// GetLocalFileServer get new local file server
func GetLocalFileServer() *LocalFileServer {
	return fserver
}
