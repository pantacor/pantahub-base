//
// Copyright 2018  Pantacor Ltd.
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
package utils

import (
	"strings"
)

// GetAdmins parses PANTAHUB_ADMINS env configuration and returns a list of Prns for
// users that shoudl have global admin powers
func GetAdmins() []Prn {
	adminsString := GetEnv(ENV_PANTAHUB_ADMINS)
	adminsStringA := strings.Split(adminsString, ",")
	prns := make([]Prn, len(adminsStringA))
	for i, v := range adminsStringA {
		prns[i] = Prn(v)
	}
	return prns
}

// GetSubscriptionAdmins parses PANTAHUB_SUBSCRIPTION ADMINS env
// configuration and returns a list of Prns for
// users that should have admin powers for processing subscription
// requsts
func GetSubscriptionAdmins() []Prn {

	adminsString := GetEnv(ENV_PANTAHUB_SUBSCRIPTION_ADMINS)
	adminsStringA := strings.Split(adminsString, ",")

	admins := GetAdmins()
	prns := make([]Prn, len(admins)+len(adminsStringA))
	for i, v := range admins {
		prns[i] = v
	}
	offset := len(admins)
	for i, v := range adminsStringA {
		prns[i+offset] = Prn(v)
	}
	return prns
}
