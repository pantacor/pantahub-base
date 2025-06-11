//
// Copyright (c) 2017-2023 Pantacor Ltd.
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

package exportservices

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/accounts/accountsdata"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/objects"
	"gitlab.com/pantacor/pantahub-base/trails/trailservices"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pvr/libpvr"
	"gitlab.com/pantacor/pvr/utils/pvjson"
	"go.mongodb.org/mongo-driver/mongo"
	"gopkg.in/mgo.v2/bson"
)

type ExportService interface {
	GetUserAccountByNick(ctx context.Context, nick string) (accounts.Account, error)
	GetDevice(ctx context.Context, nick, owner, tokenOwner string) (device *devices.Device, rerr *utils.RError)
	GetTrailObjects(ctx context.Context, deviceID, rev, owner, authType string, isPublic bool, frags string) (owa []objects.ObjectWithAccess, rerr *utils.RError)
	GetStepRev(ctx context.Context, trailID, rev, frags string) (r string, state []byte, modtime *time.Time, rerr *utils.RError)
	WriteExportTar(
		w rest.ResponseWriter,
		filename string,
		objectDownloads []objects.ObjectWithAccess,
		state []byte,
		modetime *time.Time)
}

type EService struct {
	storage *mongo.Client
	db      *mongo.Database
}

type ByObjectName []objects.ObjectWithAccess

func (a ByObjectName) Len() int           { return len(a) }
func (a ByObjectName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByObjectName) Less(i, j int) bool { return a[i].Object.ObjectName < a[j].Object.ObjectName }

func CreateService(client *mongo.Client, db string) ExportService {
	return &EService{
		storage: client,
		db:      client.Database(db),
	}
}

func (s *EService) GetStepRev(ctx context.Context, trailID, rev, frags string) (r string, state []byte, modtime *time.Time, rerr *utils.RError) {
	var err error

	trailservice := trailservices.CreateService(s.storage, s.db.Name())
	step, rerr := trailservice.GetStepRev(ctx, trailID, rev)
	if rerr != nil {
		return r, nil, nil, rerr
	}

	r = strconv.Itoa(step.Rev)

	stepState := utils.BsonUnquoteMap(&step.State)
	filteredState := stepState
	if frags != "" {
		filteredState, err = libpvr.FilterByFrags(stepState, frags)
		if err != nil {
			rerr = &utils.RError{
				Error: err.Error(),
				Code:  http.StatusInternalServerError,
			}
			return r, state, &step.TimeModified, rerr
		}
	}

	state, err = pvjson.Marshal(filteredState, pvjson.MarshalOptions{Canonical: true})
	if err != nil {
		rerr = &utils.RError{
			Error: err.Error(),
			Code:  http.StatusInternalServerError,
		}
		return r, state, &step.TimeModified, rerr
	}

	return r, state, &step.TimeModified, rerr
}

func (s *EService) GetTrailObjects(ctx context.Context, deviceID, rev, owner, authType string, isPublic bool, frags string) (owa []objects.ObjectWithAccess, rerr *utils.RError) {
	trailservice := trailservices.CreateService(s.storage, s.db.Name())

	owa, rerr = trailservice.GetTrailObjectsWithAccess(ctx, deviceID, rev, owner, authType, isPublic, frags)
	if rerr != nil {
		return owa, rerr
	}

	return owa, rerr
}

// GetUserAccountByNick : Get User Account By Nick
func (s *EService) GetUserAccountByNick(ctx context.Context, nick string) (accounts.Account, error) {
	var account accounts.Account

	account, ok := accountsdata.DefaultAccounts["prn:pantahub.com:auth:/"+nick]
	if !ok {
		collectionAccounts := s.db.Collection("pantahub_accounts")
		ctxi, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		var err error
		if bson.IsObjectIdHex(nick) {
			objID := bson.ObjectIdHex(nick)
			err = collectionAccounts.FindOne(ctxi, bson.M{"_id": objID}).Decode(&account)
		} else {
			err = collectionAccounts.FindOne(ctxi, bson.M{"nick": nick}).Decode(&account)
		}

		if err != nil {
			return account, err
		}
	}

	return account, nil
}

func (s *EService) GetDevice(ctx context.Context, nick, owner, tokenOwner string) (device *devices.Device, rerr *utils.RError) {
	collection := s.db.Collection("pantahub_devices")
	if collection == nil {
		rerr = &utils.RError{
			Error: "error with database connectivity",
			Msg:   "error with database connectivity",
			Code:  http.StatusInternalServerError,
		}
		return device, rerr
	}

	query := bson.M{
		"nick":    nick,
		"garbage": bson.M{"$ne": true},
		"owner":   owner,
	}

	if owner != tokenOwner {
		query["ispublic"] = true
	}

	ctxi, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err := collection.FindOne(ctxi, query).Decode(&device)
	if err != nil {
		rerr = &utils.RError{
			Error: err.Error(),
			Msg:   "No Access",
			Code:  http.StatusNotFound,
		}
		return device, rerr
	}

	return device, rerr
}

func (s *EService) WriteExportTar(
	w rest.ResponseWriter,
	filename string,
	objectDownloads []objects.ObjectWithAccess,
	state []byte,
	modtime *time.Time,
) {
	var fileWriter io.Writer = w

	w.Header().Add("Content-disposition", "attachment; filename="+filename)
	w.Header().Add("Content-type", "application/octet-stream")
	w.Header().Add("Pragma", "no-cache")
	w.Header().Add("Expires", "0")

	if strings.HasSuffix(strings.ToLower(filename), ".gz") ||
		strings.HasSuffix(strings.ToLower(filename), ".tgz") {
		f := gzip.NewWriter(w)
		defer f.Close()
		fileWriter = f
	}

	tw := tar.NewWriter(fileWriter)
	defer tw.Close()

	err := addToTarFileFromBytes(tw, "json", state, modtime)
	if err != nil {
		utils.RestErrorWrapper(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sort.Sort(ByObjectName(objectDownloads))

	for _, object := range objectDownloads {
		resp, err := http.Get(object.SignedGetURL)
		if err != nil {
			utils.RestErrorWrapper(w, err.Error(), http.StatusInternalServerError)
			return
		}

		err = addToTarFromResponse(tw, "objects/"+object.ID, resp, &object.TimeModified)
		if err != nil {
			utils.RestErrorWrapper(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
}

func addToTarFileFromBytes(writer *tar.Writer, archivePath string, content []byte, modtime *time.Time) error {
	file, err := os.CreateTemp(os.TempDir(), "tempfile.XXXXXXXX")
	if err != nil {
		return err
	}

	defer file.Close()
	defer os.Remove(file.Name())

	if _, err = file.Write(content); err != nil {
		return err
	}

	stat, err := os.Stat(file.Name())
	if err != nil {
		return err
	}

	if stat.IsDir() {
		return errors.New("pvr repo broken state: object file '" + file.Name() + "'is a directory")
	}

	object, err := os.Open(file.Name())
	if err != nil {
		return err
	}
	defer object.Close()

	header := new(tar.Header)
	header.Name = archivePath
	header.Size = stat.Size()
	header.Mode = int64(stat.Mode())
	header.Format = tar.FormatUSTAR
	if modtime == nil {
		header.ModTime = stat.ModTime()
	} else {
		header.ModTime = *modtime
	}

	if err = writer.WriteHeader(header); err != nil {
		return err
	}

	if _, err = io.Copy(writer, object); err != nil {
		return err
	}

	return nil
}

func addToTarFromResponse(writer *tar.Writer, archivePath string, resp *http.Response, mtime *time.Time) (err error) {
	size, err := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	if err != nil {
		return err
	}
	modtime := time.Now()
	if mtime != nil {
		modtime = *mtime
	}
	header := new(tar.Header)
	header.Name = archivePath
	header.Size = size
	header.Mode = 0600
	header.ModTime = modtime

	if err = writer.WriteHeader(header); err != nil {
		return err
	}

	if _, err = io.Copy(writer, resp.Body); err != nil {
		return err
	}

	return
}
