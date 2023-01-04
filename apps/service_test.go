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
	"reflect"
	"testing"

	jwt "github.com/pantacor/go-json-rest-middleware-jwt"
	"go.mongodb.org/mongo-driver/mongo"
)

func TestNew(t *testing.T) {
	type args struct {
		jwtMiddleware *jwt.JWTMiddleware
		mongoClient   *mongo.Client
	}
	tests := []struct {
		name string
		args args
		want *App
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := New(tt.args.jwtMiddleware, tt.args.mongoClient); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("New() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApp_setAPI(t *testing.T) {
	tests := []struct {
		name string
		app  *App
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.app.setupAPI()
		})
	}
}

func TestApp_setIndexes(t *testing.T) {
	tests := []struct {
		name    string
		app     *App
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.app.setIndexes(); (err != nil) != tt.wantErr {
				t.Errorf("App.setIndexes() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
