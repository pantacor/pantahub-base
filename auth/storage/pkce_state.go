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
	CodeChallenge       string    `json:"code_challenge" bson:"code_challenge"`
	CodeChallengeMethod string    `json:"code_challenge_method" bson:"code_challenge_method"`
	RedirectURI         string    `json:"redirect_uri" bson:"redirect_uri"`
	State               string    `json:"state" bson:"state"`
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
