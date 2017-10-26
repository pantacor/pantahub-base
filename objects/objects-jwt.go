package objects

import (
	"errors"
	"time"

	"github.com/dgrijalva/jwt-go"
	"gitlab.com/pantacor/pantahub-base/utils"
)

const (
	OBJECT_TOKEN_VALID_SEC = 86400
)

type ObjectAccessToken struct {
	// we use iss for identifying the trails endpoint
	// we use sub for identifying the requesting user wethat this claim was issued to
	// we use aud to identify the access URI in format: http://endpoint/storage-id
	// we use issued at for the time we issued this
	// we use use expires at for issuing time constrained grants
	*jwt.Token
}

type ObjectAccessClaims struct {
	jwt.StandardClaims
	DispositionName string
	Size            int64
	Method          string
}

func NewObjectAccessToken(
	name string,
	method string,
	size int64,
	issuer string,
	subject string,
	audience string,
	issuedAt int64,
	expiresAt int64) *ObjectAccessToken {
	claims := ObjectAccessClaims{
		StandardClaims: jwt.StandardClaims{
			Issuer:    issuer,
			Subject:   subject,
			Audience:  audience,
			IssuedAt:  issuedAt,
			ExpiresAt: expiresAt,
		},
		DispositionName: name,
		Size:            size,
		Method:          method,
	}

	o := &ObjectAccessToken{}
	o.Token = jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return o
}

func NewObjectAccessForSec(
	name string,
	method string,
	size int64,
	issuer string,
	subject string,
	audience string,
	validSec int64) *ObjectAccessToken {
	timeNow := time.Now().Unix()
	return NewObjectAccessToken(name, method, size, issuer, subject,
		audience, timeNow, timeNow+validSec)
}

func NewFromValidToken(encodedToken string) (*ObjectAccessToken, error) {
	claim := ObjectAccessClaims{}
	tok, err := jwt.ParseWithClaims(encodedToken, &claim, func(*jwt.Token) (interface{}, error) {
		return []byte(utils.GetEnv(utils.ENV_PANTAHUB_JWT_OBJECT_SECRET)), nil
	})

	if err != nil {
		return nil, err
	}

	if !tok.Valid {
		return nil, errors.New("Invalid Token")
	}

	objTok := &ObjectAccessToken{Token: tok}
	return objTok, nil
}

func (o *ObjectAccessToken) Sign() (string, error) {
	return o.SignedString([]byte(utils.GetEnv(utils.ENV_PANTAHUB_JWT_OBJECT_SECRET)))
}
