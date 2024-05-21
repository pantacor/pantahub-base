// Copyright 2024  Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
//	Unless required by applicable law or agreed to in writing, software
//	distributed under the License is distributed on an "AS IS" BASIS,
//	WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//	See the License for the specific language governing permissions and
//	limitations under the License.
package tokenservice

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	petname "github.com/dustinkirkland/golang-petname"
	"gitlab.com/pantacor/pantahub-base/accounts"
	"gitlab.com/pantacor/pantahub-base/tokens/tokenmodels"
	"gitlab.com/pantacor/pantahub-base/tokens/tokenrepo"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/querymongo"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type ListOfToken struct {
	querymongo.Pagination `json:",inline"`
	Items                 []tokenmodels.AuthToken `json:"items"`
}

type AuthTokenReqPayload struct {
	Name     string               `json:"name"`
	Type     accounts.AccountType `json:"type"`
	Scopes   []string             `json:"scopes"`
	ExpireAt time.Time            `json:"expire-at"`
}

type ServiceI interface {
	GetTokens(ctx context.Context, ownerPrn string, asp querymongo.ApiSearchPagination) (*ListOfToken, error)
	GetToken(ctx context.Context, id string, ownerPrn string) (*tokenmodels.AuthToken, error)
	DeleteToken(ctx context.Context, id string, ownerPrn string) error
	CreateToken(ctx context.Context, token *AuthTokenReqPayload, ownerPrn string) (*tokenmodels.AuthToken, error)
}

type Service struct {
	Repo *tokenrepo.Repo
}

// New returns a new instance of ServiceI with the provided token repository.
//
// Parameter:
// - repo: the token repository
// Return type:
// - ServiceI
func New(repo *tokenrepo.Repo) ServiceI {
	return &Service{
		Repo: repo,
	}
}

// GetTokens retrieves tokens for a specific owner based on the provided search parameters.
//
// Parameters:
// - ctx: the context in which the function is executed
// - ownerPrn: the unique identifier of the owner
// - asp: the search pagination parameters
//
// Returns:
// - *ListOfToken: a list of tokens with pagination information
// - error: an error if the operation fails
func (s *Service) GetTokens(ctx context.Context, ownerPrn string, asp querymongo.ApiSearchPagination) (*ListOfToken, error) {
	tokens, err := s.Repo.GetTokensByOwner(ctx, ownerPrn, asp)
	if err != nil {
		return nil, err
	}

	for i := range tokens {
		tokens[i].Secret = ""
		tokens[i].ParseScopes = utils.ParseScopes(tokens[i].Scopes)
	}

	response := &ListOfToken{
		Pagination: s.Repo.GetPagination(ctx, ownerPrn, asp.Filters, &asp.Url, tokens),
		Items:      tokens,
	}

	return response, nil
}

// GetToken retrieves a token by ID for a specific owner.
//
// Parameters:
// - ctx: the context in which the function is executed
// - id: the unique identifier of the token
// - ownerPrn: the unique identifier of the owner
//
// Returns:
// - *tokenmodels.AuthToken: the token information
// - error: an error if the operation fails
func (s *Service) GetToken(ctx context.Context, id string, ownerPrn string) (*tokenmodels.AuthToken, error) {
	token, err := s.Repo.GetToken(ctx, id)
	if err != nil {
		return nil, err
	}

	if ownerPrn != "" && token.Owner != ownerPrn {
		return nil, errors.New("access denied")
	}

	return token, nil
}

// CreateToken creates a new authentication token.
//
// Parameters:
// - ctx: the context in which the function is executed
// - payload: the authentication token request payload
// - ownerPrn: the unique identifier of the token owner
// Return type:
// - *tokenmodels.AuthToken: the created token
// - error: an error if the operation fails
func (s *Service) CreateToken(ctx context.Context, payload *AuthTokenReqPayload, ownerPrn string) (*tokenmodels.AuthToken, error) {
	token := &tokenmodels.AuthToken{}
	token.ID = primitive.NewObjectID()
	token.Prn = utils.IDGetPrn(token.ID, "auth-tokens")
	token.Name = payload.Name
	if payload.Type != "" {
		token.Type = payload.Type
	} else {
		token.Type = accounts.AccountTypeSessionUser
	}
	if payload.ExpireAt.IsZero() {
		token.ExpireAt = time.Now().AddDate(1, 0, 0)
	} else {
		token.ExpireAt = payload.ExpireAt
	}
	token.Owner = ownerPrn
	token.Scopes = utils.MarshalScopes(utils.ParseScopes(payload.Scopes))

	if token.Name == "" {
		token.Name = petname.Generate(3, "_")
	}

	randBytes := make([]byte, 12)
	_, err := rand.Read(randBytes)
	if err != nil {
		return nil, err
	}
	secretBytes := []byte(token.ID.Hex() + ":" + base64.RawURLEncoding.EncodeToString(randBytes))
	secret64 := base64.RawURLEncoding.EncodeToString(secretBytes)
	token.Secret = secret64
	token.SetCreatedAt()
	token.SetUpdatedAt()

	err = s.Repo.Create(ctx, token)
	if err != nil {
		return nil, err
	}

	return token, nil
}

// DeleteToken deletes a token based on the provided ID if the owner matches.
//
// Parameters:
// - ctx: the context in which the function is executed
// - id: the unique identifier of the token to delete
// - ownerPrn: the unique identifier of the token owner
// Return type:
// - error: an error if the operation fails
func (s *Service) DeleteToken(ctx context.Context, id string, ownerPrn string) error {
	token, err := s.Repo.GetToken(ctx, id)
	if err != nil {
		return err
	}

	if token.Owner != ownerPrn {
		return errors.New("access denied")
	}

	err = s.Repo.DeleteToken(ctx, id)
	if err != nil {
		return err
	}

	return nil
}
