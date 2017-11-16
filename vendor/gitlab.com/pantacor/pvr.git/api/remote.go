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
package pvrapi

type PvrRemote struct {
	RemoteSpec         string   `json:"pvr-spec"`         // the pvr remote protocol spec available
	JsonGetUrl         string   `json:"json-get-url"`     // where to pvr post stuff
	JsonKey            string   `json:"json-key"`         // what key is to use in post json [default: json]
	ObjectsEndpointUrl string   `json:"objects-endpoint"` // where to store/retrieve objects
	PostUrl            string   `json:"post-url"`         // where to post/announce new revisions
	PostFields         []string `json:"post-fields"`      // what fields require input
	PostFieldsOpt      []string `json:"post-fields-opt"`  // what optional fields are available [default: <empty>]
}

type PvrRemoteObject struct {
	Id           string `json:"id" bson:"id"`
	StorageId    string `json:"storage-id" bson:"_id"`
	Owner        string `json:"owner"`
	ObjectName   string `json:"objectname"`
	Sha          string `json:"sha256sum"`
	Size         string `json:"size"`
	MimeType     string `json:"mime-type"`
	SignedPutUrl string `json:"signed-puturl"`
	SignedGetUrl string `json:"signed-geturl"`
	Now          string `json:"now"`
	ExpireTime   string `json:"expire-time"`
}
