// Copyright 2020  Pantacor Ltd.
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

// Package apps package to manage extensions of the oauth protocol
package apps

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/ant0ine/go-json-rest/rest"
	"github.com/ant0ine/go-json-rest/rest/test"
	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"gitlab.com/pantacor/pantahub-base/utils"
)

const TOKEN = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJleHAiOjI1NzY3MDM3MTMsImlkIjoiaGlnaGVyLnZuZkBnbWFpbC5jb20iLCJuaWNrIjoiaGlnaGVyY29tdmUiLCJvcmlnX2lhdCI6MTU3NjY5MjkxMywicHJuIjoicHJuOjo6YWNjb3VudHM6LzVjOGY5M2RjZWVhODIzMDAwODc2YzRmYSIsInJvbGVzIjoidXNlciIsInNjb3BlcyI6InBybjpwYW50YWh1Yi5jb206YXBpczovYmFzZS9hbGwiLCJ0eXBlIjoiVVNFUiJ9.B3lnQR0UJDJdHvZVSkbFL7mzh4mQFdWiBikn68h1cdo"

func TestApp_handleCreateApp(t *testing.T) {
	utils.InitScopes()
	client, err := utils.GetMongoClientTest()
	if err != nil {
		t.Error(err)
		return
	}
	app := new(App)
	app.jwtMiddleware = &jwt.JWTMiddleware{
		Key:              []byte("1234567890"),
		Realm:            "pantahub services",
		SigningAlgorithm: "HS256",
		Authenticator:    func(userId string, password string) bool { return true },
	}
	app.mongoClient = client

	app.API = rest.NewApi()
	app.API.Use(app.jwtMiddleware)

	router, err := rest.MakeRouter(
		rest.Post("/", app.handleCreateApp),
	)

	if err != nil {
		t.Error(err)
		return
	}
	app.API.SetApp(router)

	type args struct {
		body interface{}
	}
	tests := []struct {
		name   string
		app    *App
		args   args
		expect func(raw []byte) error
	}{
		{
			name: "Error on unsopported scopes",
			app:  app,
			args: args{
				body: map[string]interface{}{
					"type":          "something random",
					"scopes":        []string{string(utils.Scopes.API.ID)},
					"redirect_uris": []string{"algo"},
				},
			},
			expect: func(raw []byte) error {
				body := make(map[string]interface{})
				json.Unmarshal(raw, &body)
				if body["Error"] == "Invalid app type" {
					return nil
				}
				return errors.New("should not be supported")
			},
		},
		{
			name: "Error on unsopported scopes",
			app:  app,
			args: args{
				body: map[string]interface{}{
					"type":          AppTypePublic,
					"scopes":        []string{string(utils.Scopes.API.ID)},
					"redirect_uris": []string{"algo"},
				},
			},
			expect: func(raw []byte) error {
				body := make(map[string]interface{})
				json.Unmarshal(raw, &body)
				if body["Error"] == "Scopes are invalid" {
					return nil
				}
				return errors.New("should not be supported")
			},
		},
		{
			name: "Pass with correct scopes",
			app:  app,
			args: args{
				body: CreateAppPayload{
					Type:         AppTypePublic,
					RedirectURIs: []string{"redirect uri 1"},
					Scopes: []utils.Scope{
						utils.Scope{
							ID:      "all",
							Service: "",
						},
					},
				},
			},
			expect: func(raw []byte) error {
				body := make(map[string]interface{})
				json.Unmarshal(raw, &body)
				if body["Error"] != "Scopes are invalid" {
					return nil
				}
				return errors.New("should not be supported")
			},
		},
		{
			name: "Fail is redirect uris are empty",
			app:  app,
			args: args{
				body: CreateAppPayload{
					Type:         AppTypePublic,
					RedirectURIs: []string{},
					Scopes: []utils.Scope{
						utils.Scope{
							ID:      "all",
							Service: "self",
						},
					},
				},
			},
			expect: func(raw []byte) error {
				body := make(map[string]interface{})
				json.Unmarshal(raw, &body)
				if body["Error"] == "A new app need to have at least one redirect URI" {
					return nil
				}

				return errors.New("Fail")
			},
		},
		{
			name: "Only allow to use empty service or pantahub space",
			app:  app,
			args: args{
				body: CreateAppPayload{
					Type:         AppTypePublic,
					RedirectURIs: []string{"redirect uri 1"},
					Scopes: []utils.Scope{
						utils.Scope{
							ID:      "all",
							Service: "self",
						},
					},
				},
			},
			expect: func(raw []byte) error {
				body := make(map[string]interface{})
				json.Unmarshal(raw, &body)
				if body["Error"] == "Scopes are invalid" {
					return nil
				}

				return errors.New("should not be supported")
			},
		},
		{
			name: "Pass custom scopes",
			app:  app,
			args: args{
				body: CreateAppPayload{
					Type:         AppTypePublic,
					RedirectURIs: []string{"redirect uri 1"},
					Scopes: []utils.Scope{
						utils.Scope{
							ID:      "all",
							Service: "",
						},
					},
				},
			},
			expect: func(raw []byte) error {
				body := make(map[string]interface{})
				json.Unmarshal(raw, &body)
				if body["Error"] != "" {
					return nil
				}

				scope := body["scopes"].([]map[string]interface{})[0]
				if scope["service"] != utils.PantahubServiceID && scope["service"] != "" {
					return nil
				}

				return errors.New("Scopes should be supported")
			},
		},
		{
			name: "Pass ph scopes",
			app:  app,
			args: args{
				body: CreateAppPayload{
					Type:         AppTypePublic,
					RedirectURIs: []string{"redirect uri 1"},
					Scopes:       []utils.Scope{utils.Scopes.API},
				},
			},
			expect: func(raw []byte) error {
				body := make(map[string]interface{})
				json.Unmarshal(raw, &body)
				if body["Error"] != "" {
					return nil
				}

				scope := body["scopes"].([]map[string]interface{})[0]
				if scope["service"] == utils.PantahubServiceID && scope["service"] != "" {
					return nil
				}

				return errors.New("Scopes should be supported")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := test.MakeSimpleRequest("POST", "http://1.2.3.4/", tt.args.body)
			request.Header.Set("Authorization", "Bearer "+TOKEN)
			recorded := test.RunRequest(t, app.API.MakeHandler(), request)
			raw, err := recorded.DecodedBody()
			if err != nil {
				t.Error(tt.name + "::" + err.Error())
				return
			}
			err = tt.expect(raw)
			if err != nil {
				t.Error(tt.name + "::" + err.Error())
				return
			}
		})
	}
}

func Test_validatePayload(t *testing.T) {
	type args struct {
		app *CreateAppPayload
	}
	tests := []struct {
		name     string
		args     args
		wantErr  bool
		response interface{}
	}{
		{
			name: "t",
			args: args{
				app: &CreateAppPayload{
					Type: "",
					Scopes: []utils.Scope{utils.Scope{
						ID: "notvalid",
					}},
					RedirectURIs: []string{"something.com"},
				},
			},
			wantErr: true,
		},
		{
			name: "t",
			args: args{
				app: &CreateAppPayload{
					Type: string(AppTypeConfidential),
					Scopes: []utils.Scope{utils.Scope{
						ID: "notvalid",
					}},
					RedirectURIs: []string{"something.com"},
				},
			},
			wantErr: false,
		},
		{
			name: "t",
			args: args{
				app: &CreateAppPayload{
					Type:         string(AppTypeConfidential),
					Scopes:       []utils.Scope{utils.Scopes.ReadUser},
					RedirectURIs: []string{"something.com"},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validatePayload(tt.args.app); (err != nil) != tt.wantErr {
				t.Errorf("validatePayload() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
