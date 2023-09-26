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

package trailmodels

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// PvrRemote pvr remote specification payload
type PvrRemote struct {
	RemoteSpec         string   `json:"pvr-spec"`         // the pvr remote protocol spec available
	JSONGetURL         string   `json:"json-get-url"`     // where to pvr post stuff
	JSONKey            string   `json:"json-key"`         // what key is to use in post json [default: json]
	ObjectsEndpointURL string   `json:"objects-endpoint"` // where to store/retrieve objects
	PostURL            string   `json:"post-url"`         // where to post/announce new revisions
	PostFields         []string `json:"post-fields"`      // what fields require input
	PostFieldsOpt      []string `json:"post-fields-opt"`  // what optional fields are available [default: <empty>]
	StepGetUrl         string   `json:"step-get-url"`     // where to get the latest step status
}

// Trail define the structure of a trail
type Trail struct {
	ID     primitive.ObjectID `json:"id" bson:"_id"`
	Owner  string             `json:"owner"`
	Device string             `json:"device"`
	//  Admins   []string `json:"admins"`   // XXX: maybe this is best way to do delegating device access....
	LastInSync   time.Time              `json:"last-insync" bson:"last-insync"`
	LastTouched  time.Time              `json:"last-touched" bson:"last-touched"`
	FactoryState map[string]interface{} `json:"factory-state" bson:"factory-state"`
	UsedObjects  []string               `bson:"used_objects" json:"used_objects"`
}

// Step wanted can be added by the device owner or delegate.
// steps that were not reported can be deleted still. other steps
// cannot be deleted until the device gets deleted as well.
type Step struct {
	ID                  string                 `json:"id" bson:"_id"` // XXX: make type
	Owner               string                 `json:"owner"`
	Device              string                 `json:"device"`
	Committer           string                 `json:"committer"`
	TrailID             primitive.ObjectID     `json:"trail-id" bson:"trail-id"` //parent id
	Rev                 int                    `json:"rev"`
	CommitMsg           string                 `json:"commit-msg" bson:"commit-msg"`
	State               map[string]interface{} `json:"state"` // json blurb
	StateSha            string                 `json:"state-sha" bson:"statesha"`
	StepProgress        StepProgress           `json:"progress" bson:"progress"`
	StepTime            time.Time              `json:"step-time" bson:"step-time"`
	ProgressTime        time.Time              `json:"progress-time" bson:"progress-time"`
	Meta                map[string]interface{} `json:"meta"` // json blurb
	UsedObjects         []string               `bson:"used_objects" json:"used_objects"`
	IsPublic            bool                   `json:"-" bson:"ispublic"`
	MarkPublicProcessed bool                   `json:"mark_public_processed" bson:"mark_public_processed"`
	Garbage             bool                   `json:"garbage" bson:"garbage"`
	TimeCreated         time.Time              `json:"time-created" bson:"timecreated"`
	TimeModified        time.Time              `json:"time-modified" bson:"timemodified"`
}

// StepProgress progression of a step
type StepProgress struct {
	Progress  int              `json:"progress"`                    // progress number. steps or 1-100
	Downloads DownloadProgress `json:"downloads" bson:"downloads"`  // progress number. steps or 1-100
	StatusMsg string           `json:"status-msg" bson:"statusmsg"` // message of progress status
	Data      interface{}      `json:"data,omitempty" bson:"data"`  // data field that can hold things the device wants to remember
	Status    string           `json:"status"`                      // status code
	Log       string           `json:"log"`                         // log if available
}

// DownloadProgress holds info about total and individual download progress
type DownloadProgress struct {
	Total   ObjectProgress   `json:"total" bson:"total"`
	Objects []ObjectProgress `json:"objects" bson:"objects"`
}

// ObjectProgress holds info object download progress
type ObjectProgress struct {
	ObjectName      string `json:"object_name,omitempty" bson:"object_name,omitempty"`
	ObjectID        string `json:"object_id,omitempty" bson:"object_id,omitempty"`
	TotalSize       int64  `json:"total_size" bson:"total_size"`
	StartTime       int64  `json:"start_time" bson:"start_time"`
	CurrentTime     int64  `json:"current_time" bson:"currentb_time"`
	TotalDownloaded int64  `json:"total_downloaded" bson:"total_downloaded"`
}

// TrailSummary details about a trail
type TrailSummary struct {
	DeviceID         string    `json:"deviceid" bson:"deviceid"`
	Device           string    `json:"device" bson:"device"`
	DeviceNick       string    `json:"device-nick" bson:"device_nick"`
	Rev              int       `json:"revision" bson:"revision"`
	ProgressRev      int       `json:"progress-revision" bson:"progress_revision"`
	Progress         int       `json:"progress" bson:"progress"` // progress number. steps or 1-100
	IsPublic         bool      `json:"public" bson:"public"`
	StateSha         string    `json:"state-sha" bson:"state_sha256"`
	StatusMsg        string    `json:"status-msg" bson:"status_msg"` // message of progress status
	Status           string    `json:"status" bson:"status"`         // status code
	Timestamp        time.Time `json:"timestamp" bson:"timestamp"`   // greater of last seen and last modified
	StepTime         time.Time `json:"step-time" bson:"step_time"`
	ProgressTime     time.Time `json:"progress-time" bson:"progress_time"`
	TrailTouchedTime time.Time `json:"trail-touched-time" bson:"trail_touched_time"`
	RealIP           string    `json:"real-ip" bson:"real_ip"`
	FleetGroup       string    `json:"fleet-group" bson:"fleet_group"`
	FleetModel       string    `json:"fleet-model" bson:"fleet_model"`
	FleetLocation    string    `json:"fleet-location" bson:"fleet_location"`
	FleetRev         string    `json:"fleet-rev" bson:"fleet_rev"`
	Owner            string    `json:"-" bson:"owner"`
}
