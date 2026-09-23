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
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"gitlab.com/pantacor/pantahub-base/auth/redirecturi"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ErrAppNotOwned is returned when the application to change does not exist or
// belongs to somebody else. The two are not told apart on purpose.
var ErrAppNotOwned = errors.New("application not found")

// ErrInvalidDetails wraps every refusal that is about the requested change
// itself. Its message is meant for whoever asked; any other error is not.
var ErrInvalidDetails = errors.New("invalid application details")

func invalidDetails(msg string) error {
	return fmt.Errorf("%w: %s", ErrInvalidDetails, msg)
}

// maxAppNameLength bounds a display name. It is shown on the consent page.
const maxAppNameLength = 120

// Details are the fields of an application that can be changed without
// touching its identity or its credentials. A nil field is left as it is.
type Details struct {
	Name         *string
	RedirectURIs *[]string
}

// UpdateDetails changes the display name and the callback URLs of one of
// owner's applications, and nothing else.
//
// It is deliberately narrower than PUT /apps/:id. That endpoint replaces the
// whole record, which can rename the application (changing its client id),
// switch its type (minting a client secret) and rewrite its scopes. None of
// that is wanted from a caller that must never see or cause a secret, so this
// is a targeted $set rather than a call into CreateOrUpdateApp, which also
// rewrites the secret fields depending on the type.
func UpdateDetails(ctx context.Context, database *mongo.Database, owner, id string, details Details) (*TPApp, error) {
	if owner == "" {
		return nil, ErrAppNotOwned
	}

	set := bson.M{}
	if details.Name != nil {
		name := strings.TrimSpace(*details.Name)
		if name == "" {
			return nil, invalidDetails("name cannot be empty")
		}
		if len([]rune(name)) > maxAppNameLength {
			return nil, invalidDetails("name is too long")
		}
		set["name"] = name
	}
	if details.RedirectURIs != nil {
		uris := *details.RedirectURIs
		// An application without callbacks is left unconstrained by the
		// redirect check for compatibility with old registrations, so emptying
		// the list would widen where its codes may be sent, not narrow it.
		if len(uris) == 0 {
			return nil, invalidDetails("an application needs at least one redirect URI")
		}
		for _, uri := range uris {
			if err := redirecturi.ValidateURI(uri); err != nil {
				return nil, invalidDetails("invalid redirect URI '" + uri + "': " + err.Error())
			}
		}
		set["redirect_uris"] = uris
	}
	if len(set) == 0 {
		return nil, invalidDetails("nothing to update")
	}

	updated, httpCode, err := UpdateApp(ctx, owner, id, set, database)
	if httpCode == http.StatusNotFound {
		return nil, ErrAppNotOwned
	}
	if err != nil {
		return nil, err
	}
	return updated, nil
}
