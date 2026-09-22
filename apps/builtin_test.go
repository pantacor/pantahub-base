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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/pantacor/pantahub-base/utils"
)

func TestPvrIsBuiltIn(t *testing.T) {
	if len(utils.PhScopeArray) == 0 {
		utils.InitScopes()
	}
	for _, id := range []string{"prn:pantahub.com:apis:/pvr", "pvr"} {
		app := builtinApp(id)
		require.NotNil(t, app, id)
		assert.Equal(t, "prn:pantahub.com:apis:/pvr", app.Prn)
		assert.Equal(t, AppTypePKCE, app.Type)
		assert.False(t, app.Dynamic)
		assert.Empty(t, app.SecretHash)
		assert.Contains(t, app.RedirectURIs, "http://127.0.0.1/callback")
		assert.Contains(t, utils.MarshalScopes(app.Scopes), "prn:pantahub.com:apis:/base/all")
	}

	// A caller's copy of the table cannot change the next lookup.
	app := builtinApp("pvr")
	app.RedirectURIs[0] = "https://evil.example.com/cb"
	again := builtinApp("pvr")
	assert.NotContains(t, again.RedirectURIs, "https://evil.example.com/cb")
}

func TestBuiltInNicksAreReserved(t *testing.T) {
	assert.True(t, reservedNick("pvr"))
	assert.True(t, reservedNick("PVR"))
	assert.False(t, reservedNick("pvr-tools"))
	assert.False(t, reservedNick("hubmanager"))
}
