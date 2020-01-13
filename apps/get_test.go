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
	"testing"

	"github.com/ant0ine/go-json-rest/rest"
)

func TestApp_handleGetApp(t *testing.T) {
	type args struct {
		w rest.ResponseWriter
		r *rest.Request
	}
	tests := []struct {
		name string
		app  *App
		args args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.app.handleGetApp(tt.args.w, tt.args.r)
		})
	}
}

func TestApp_handleGetApps(t *testing.T) {
	type args struct {
		w rest.ResponseWriter
		r *rest.Request
	}
	tests := []struct {
		name string
		app  *App
		args args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.app.handleGetApps(tt.args.w, tt.args.r)
		})
	}
}
