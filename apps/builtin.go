// Copyright (c) 2026 Pantacor Ltd.
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

package apps

import (
	"strings"
	"time"

	"github.com/gosimple/slug"
	"gitlab.com/pantacor/pantahub-base/utils"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// PvrNick is the nick of the pvr CLI, whose client id every pvr binary
// hardcodes as prn:pantahub.com:apis:/pvr.
const PvrNick = "pvr"

// builtinApps are first-party public clients that exist on every deployment
// without anybody creating them. They answer only where no application with
// that nick is stored, so one created before they were built in keeps
// working as it was. A client id is derived from a nick, so the nick is
// reserved: no account can create another one under it.
var builtinApps = []TPApp{
	{
		ID:   builtinObjectID("000000000000000000000001"),
		Name: "pvr",
		Nick: PvrNick,
		Prn:  utils.BuildScopePrn(PvrNick),
		Type: AppTypePKCE,
		// pvr listens on a random loopback port; loopback ports and hosts are
		// not compared (RFC 8252 section 7.3).
		RedirectURIs: []string{"http://127.0.0.1/callback", "http://localhost/callback"},
		TimeCreated:  time.Date(2026, time.September, 22, 0, 0, 0, 0, time.UTC),
		TimeModified: time.Date(2026, time.September, 22, 0, 0, 0, 0, time.UTC),
	},
}

func builtinObjectID(hex string) primitive.ObjectID {
	id, err := primitive.ObjectIDFromHex(hex)
	if err != nil {
		panic(err)
	}
	return id
}

// builtinApp returns the built-in client id names, by prn, nick or id.
func builtinApp(id string) *TPApp {
	for i := range builtinApps {
		app := builtinApps[i]
		if id == app.Prn || id == app.Nick || id == app.ID.Hex() {
			// The scope table is filled by utils.InitScopes at startup, after
			// this one is built.
			app.Scopes = utils.PhScopeArray
			app.RedirectURIs = append([]string(nil), app.RedirectURIs...)
			return &app
		}
	}
	return nil
}

// reservedNick reports whether nick belongs to a built-in client, so that no
// account can create or rename an application to it.
func reservedNick(nick string) bool {
	nick = strings.ToLower(slug.Make(nick))
	for _, app := range builtinApps {
		if nick == app.Nick {
			return true
		}
	}
	return false
}
