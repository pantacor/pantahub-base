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

// Package utils package to manage extensions of the oauth protocol
package utils

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"

	"github.com/ant0ine/go-json-rest/rest"
	"github.com/gosimple/slug"
)

const (
	// BaseServiceID all services id base string
	BaseServiceID string = "prn:pantahub.com:apis:/"

	// PantahubServiceID Pantahub service ID
	PantahubServiceID string = "prn:pantahub.com:apis:/base"
)

// Scope scope structure
type Scope struct {
	ID          string `json:"id" bson:"id"`
	Service     string `json:"service" bson:"service"`
	Description string `json:"description" bson:"description"`
	Required    bool   `json:"required" bson:"required"`
}

// IScopes define every possible scope type
type IScopes struct {
	API           Scope
	ReadUser      Scope
	WriteUser     Scope
	Devices       Scope
	ReadDevices   Scope
	WriteDevices  Scope
	UpdateDevices Scope
	Objects       Scope
	ReadObjects   Scope
	WriteObjects  Scope
	UpdateObjects Scope
	Trails        Scope
	ReadTrails    Scope
	WriteTrails   Scope
	UpdateTrails  Scope
	Metrics       Scope
	ReadMetrics   Scope
	WriteMetrics  Scope
	UpdateMetrics Scope
}

// Scopes variable with all the posible scopes
var Scopes = &IScopes{
	API: Scope{
		ID:          "all",
		Service:     PantahubServiceID,
		Description: "Complete Access",
	},
	Devices: Scope{
		ID:          "devices",
		Service:     PantahubServiceID,
		Description: "Read/Write devices",
	},
	ReadDevices: Scope{
		ID:          "devices.readonly",
		Service:     PantahubServiceID,
		Description: "Read only devices",
	},
	WriteDevices: Scope{
		ID:          "devices.write",
		Service:     PantahubServiceID,
		Description: "Write only devices",
	},
	UpdateDevices: Scope{
		ID:          "devices.change",
		Service:     PantahubServiceID,
		Description: "Update devices",
	},
	ReadUser: Scope{
		ID:          "user.readonly",
		Service:     PantahubServiceID,
		Description: "Read only user",
	},
	WriteUser: Scope{
		ID:          "user.write",
		Service:     PantahubServiceID,
		Description: "Write only user",
	},
	Trails: Scope{
		ID:          "trails",
		Service:     PantahubServiceID,
		Description: "Read/Write only trails",
	},
	ReadTrails: Scope{
		ID:          "trails.readonly",
		Service:     PantahubServiceID,
		Description: "Read only trails",
	},
	WriteTrails: Scope{
		ID:          "trails.write",
		Service:     PantahubServiceID,
		Description: "Write only trails",
	},
	UpdateTrails: Scope{
		ID:          "trails.change",
		Service:     PantahubServiceID,
		Description: "Update trails",
	},
	Objects: Scope{
		ID:          "objects",
		Service:     PantahubServiceID,
		Description: "Read/Write only objects",
	},
	ReadObjects: Scope{
		ID:          "objects.readonly",
		Service:     PantahubServiceID,
		Description: "Read only objects",
	},
	WriteObjects: Scope{
		ID:          "objects.write",
		Service:     PantahubServiceID,
		Description: "Write only objects",
	},
	UpdateObjects: Scope{
		ID:          "objects.change",
		Service:     PantahubServiceID,
		Description: "Update objects",
	},
	Metrics: Scope{
		ID:          "metrics",
		Service:     PantahubServiceID,
		Description: "Read/Write only metrics",
	},
	ReadMetrics: Scope{
		ID:          "metrics.readonly",
		Service:     PantahubServiceID,
		Description: "Read only metrics",
	},
	WriteMetrics: Scope{
		ID:          "metrics.write",
		Service:     PantahubServiceID,
		Description: "Write only metrics",
	},
	UpdateMetrics: Scope{
		ID:          "metrics.change",
		Service:     PantahubServiceID,
		Description: "Update metrics",
	},
}

// PhScopeNames List of pantahub base scope names
var PhScopeNames []string = []string{}

// PhScopeArray List of pantahub base scope names
var PhScopeArray []Scope = []Scope{}

// PhScopesMap Map of all scope by type
var PhScopesMap map[string]Scope = map[string]Scope{}

// InitScopes get all scopes names
func InitScopes() {
	val := reflect.ValueOf(Scopes).Elem()
	for i := 0; i < val.NumField(); i++ {
		id := fmt.Sprintf("%s", val.Field(i).FieldByName("ID"))
		scope := val.Field(i).Interface().(Scope)
		PhScopeNames = append(PhScopeNames, PantahubServiceID+"/"+id)
		PhScopeArray = append(PhScopeArray, scope)
		PhScopesMap[id] = scope
	}
}

//ScopeFilter :  Scope Filter for end points
func ScopeFilter(filterScopes []Scope, handler rest.HandlerFunc) rest.HandlerFunc {
	parsedFilterScopes := ParseScopes(filterScopes)

	return func(w rest.ResponseWriter, r *rest.Request) {
		authInfo := GetAuthInfo(r)
		if authInfo != nil && len(parsedFilterScopes) > 0 {
			if !MatchScope(parsedFilterScopes, authInfo.Scopes) {
				phAuth := GetEnv(ENV_PANTAHUB_AUTH)
				w.Header().Set("WWW-Authenticate", `Bearer Realm="pantahub services",
								ph-aeps="`+phAuth+`",
								scope="`+strings.Join(parsedFilterScopes, " ")+`",
								error="insufficient_scope",
								error_description="The request requires higher privileges than provided by the
				     access token"
								`)
				RestErrorWrapper(w, "InSufficient Scopes", http.StatusForbidden)
				return
			}
		}
		handler(w, r)
	}
}

// MatchScope serch one scope in all the available scopes
func MatchScope(filterScopes []string, requestScopes []string) bool {
	for _, fs := range filterScopes {
		for _, rs := range requestScopes {
			if fs == rs {
				return true
			}
		}
	}
	return false
}

// MatchAllScope serch one scope in all the available scopes
func MatchAllScope(filterScopes []string, requestScopes []string) bool {
	allOnRequest := true
	for _, fs := range filterScopes {
		isOnRequest := false
		for _, rs := range requestScopes {
			if fs == rs {
				isOnRequest = isOnRequest || true
				break
			}
		}
		allOnRequest = allOnRequest && isOnRequest
		if !allOnRequest {
			break
		}
	}
	return allOnRequest
}

// ParseScopes covert array of scopes on array of string scopes
func ParseScopes(scopes []Scope) []string {
	parsedScopes := make([]string, len(scopes))
	for k, scope := range scopes {
		parsedScopes[k] = scope.Service + "/" + scope.ID
	}
	return parsedScopes
}

// BuildScopePrn build a scope PRN from a service ID
func BuildScopePrn(serviceID string) string {
	return BaseServiceID + slug.Make(serviceID)
}

// ScopeFilterBy filter and array of scopes using a function
func ScopeFilterBy(scopes []Scope, f func(scope *Scope, i int) bool) []Scope {
	fapps := make([]Scope, 0)
	for i, s := range scopes {
		if f(&s, i) {
			fapps = append(fapps, s)
		}
	}
	return fapps
}
