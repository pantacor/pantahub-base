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
//

// Package apps package to manage extensions of the oauth protocol
package apps

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"gitlab.com/pantacor/pantahub-base/utils/jwtauth"
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
	app.jwtConfig = &jwtauth.Config{
		Key:              []byte("1234567890"),
		Realm:            "pantahub services",
		SigningAlgorithm: "HS256",
	}
	app.mongoClient = client

	e := echoutil.New()
	e.POST("/", app.handleCreateApp, echoutil.JWT(app.jwtConfig))

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
						{
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
						{
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
						{
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
						{
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
			b, err := json.Marshal(tt.args.body)
			if err != nil {
				t.Error(tt.name + "::" + err.Error())
				return
			}
			req := httptest.NewRequest(http.MethodPost, "http://1.2.3.4/", bytes.NewReader(b))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+TOKEN)
			rec := httptest.NewRecorder()
			e.ServeHTTP(rec, req)
			err = tt.expect(rec.Body.Bytes())
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
					Scopes: []utils.Scope{
						{
							ID: "notvalid",
						},
					},
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
					Scopes: []utils.Scope{
						{
							ID: "notvalid",
						},
					},
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
		{
			name: "t",
			args: args{
				app: &CreateAppPayload{
					Type:         string(AppTypeConfidential),
					Scopes:       []utils.Scope{utils.Scopes.ReadUser},
					RedirectURIs: []string{"something.com"},
					Logo:         utils.ImageLogo,
				},
			},
			wantErr: true,
		},
		{
			name: "t",
			args: args{
				app: &CreateAppPayload{
					Type:         string(AppTypeConfidential),
					Scopes:       []utils.Scope{utils.Scopes.ReadUser},
					RedirectURIs: []string{"something.com"},
					Logo:         utils.ImageLinkedin,
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
