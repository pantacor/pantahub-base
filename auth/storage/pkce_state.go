// Copyright (c) 2017-2026 Pantacor Ltd.
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

package storage

import (
	"time"

	"gitlab.com/pantacor/pantahub-base/utils/models"
)

const PKCEServicePrn = "pkce_states"

// PKCEState represents the state for a PKCE authorization flow
type PKCEState struct {
	models.Timestamp      `json:",inline" bson:",inline"`
	models.Identification `json:",inline" bson:",inline"`
	models.Ownership      `json:",inline" bson:",inline"`

	AuthCode            string    `json:"auth_code" bson:"auth_code"`
	SessionID           string    `json:"session_id" bson:"session_id"`
	UserCode            string    `json:"user_code" bson:"user_code"`
	CodeChallenge       string    `json:"code_challenge" bson:"code_challenge"`
	CodeChallengeMethod string    `json:"code_challenge_method" bson:"code_challenge_method"`
	RedirectURI         string    `json:"redirect_uri" bson:"redirect_uri"`
	State               string    `json:"state" bson:"state"`
	Scope               string    `json:"scope" bson:"scope"`
	ClientID            string    `json:"client_id" bson:"client_id"`
	Token               string    `json:"token" bson:"token"`
	LastPollAt          time.Time `json:"last_poll_at" bson:"last_poll_at"`
	Interval            int       `json:"interval" bson:"interval"`
	ExpiresAt           time.Time `json:"expires_at" bson:"expires_at"`
	IsUsed              bool      `json:"is_used" bson:"is_used"`
	UserID              string    `json:"user_id" bson:"user_id"`
	WorkspaceID         string    `json:"workspace_id" bson:"workspace_id"`
}

func (pks *PKCEState) GetServicePrn() string {
	return PKCEServicePrn
}

// NewPKCEState creates a new PKCEState object
func NewPKCEState() *PKCEState {
	return &PKCEState{
		Identification: models.NewIdentification(PKCEServicePrn),
		Timestamp:      models.NewTimeStamp(),
	}
}
