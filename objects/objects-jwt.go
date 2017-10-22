package objects

import (
	"time"

	"github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
)

const (
	OBJECT_TOKEN_VALID_SEC = 60
)

type ObjectAccessToken struct {
	// we use iss for identifying the trails endpoint
	// we use sub for identifying the requesting user wethat this claim was issued to
	// we use aud to identify the access URI in format: http://endpoint/storage-id
	// we use issued at for the time we issued this
	// we use use expires at for issuing time constrained grants
	*jwt.Token
}

func NewObjectAccessToken(
	issuer string,
	subject string,
	audience string,
	issuedAt int64,
	expiresAt int64) *ObjectAccessToken {
	claims := jwt.StandardClaims{
		Issuer:    issuer,
		Subject:   subject,
		Audience:  audience,
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Unix() + expiresAt,
	}

	o := &ObjectAccessToken{}
	o.Token = jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return o
}

func NewObjectAccessForSec(
	issuer string,
	subject string,
	audience string,
	validSec int64) *ObjectAccessToken {
	timeNow := time.Now().Unix()
	return NewObjectAccessToken(issuer, subject, audience, timeNow, timeNow+validSec)
}

func (o *ObjectAccessToken) LoadValidToken(encodedToken string) (*jwt.Token, error) {
	tok, err := jwt.Parse(encodedToken, func(*jwt.Token) (interface{}, error) {
		return utils.GetEnv(utils.ENV_PANTAHUB_JWT_OBJECT_SECRET), nil
	})

	return tok, err
}

func (o *ObjectAccessToken) Sign(token *jwt.Token) (string, error) {
	return o.SignedString(utils.GetEnv(utils.ENV_PANTAHUB_JWT_OBJECT_SECRET))
}
