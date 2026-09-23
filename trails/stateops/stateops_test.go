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

package stateops

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gitlab.com/pantacor/pvr/utils/pvjson"
)

// signed.json is pvr's own fixture: an nginx app signed by _sigs/nginx.json
// and its configuration signed apart by _sigs/nginx_config.json.
func signedState(t *testing.T) map[string]interface{} {
	t.Helper()
	buf, err := os.ReadFile("testdata/signed.json")
	require.NoError(t, err)
	state := map[string]interface{}{}
	require.NoError(t, pvjson.Unmarshal(buf, &state))
	return state
}

func plainState() map[string]interface{} {
	return map[string]interface{}{
		"#spec":             "pantavisor-service-system@1",
		"bsp/kernel.img":    "1111111111111111111111111111111111111111111111111111111111111111",
		"bsp/run.json":      map[string]interface{}{"initrd": "pantavisor"},
		"web/root.squashfs": "2222222222222222222222222222222222222222222222222222222222222222",
		"web/run.json":      map[string]interface{}{"name": "web"},
		"web/old.bin":       "3333333333333333333333333333333333333333333333333333333333333333",
		"_config/web/a.cfg": "4444444444444444444444444444444444444444444444444444444444444444",
		"storage.json":      map[string]interface{}{"disks": []interface{}{}},
	}
}

func partNamed(parts []Part, name string) *Part {
	for i := range parts {
		if parts[i].Name == name {
			return &parts[i]
		}
	}
	return nil
}

func TestPartsGroupKeysAndNameTheirSignatures(t *testing.T) {
	parts := Parts(signedState(t))

	nginx := partNamed(parts, "nginx")
	require.NotNil(t, nginx)
	assert.Equal(t, "app", nginx.Kind)
	assert.Contains(t, nginx.Files, "nginx/root.squashfs")
	assert.Equal(t, []string{"_sigs/nginx.json"}, nginx.SignedBy)

	config := partNamed(parts, "_config/nginx")
	require.NotNil(t, config)
	assert.Equal(t, "config", config.Kind)
	assert.ElementsMatch(t, []string{"_sigs/nginx.json", "_sigs/nginx_config.json"}, config.SignedBy,
		"one file of it is signed with the app, the rest apart")

	assert.Equal(t, "signature", partNamed(parts, "_sigs/nginx.json").Kind)
	assert.Equal(t, "document", partNamed(parts, "#spec").Kind)
}

func TestRemovePartTakesItsConfigAndSignature(t *testing.T) {
	before := plainState()
	after, err := RemoveParts(before, []string{"web/"})
	require.NoError(t, err)

	for key := range after {
		assert.False(t, strings.HasPrefix(key, "web/"), key)
		assert.False(t, strings.HasPrefix(key, "_config/web/"), key)
	}
	assert.Contains(t, after, "bsp/kernel.img")
	assert.Contains(t, before, "web/run.json", "the input is left alone")

	_, err = RemoveParts(before, []string{"nope"})
	assert.True(t, errors.Is(err, ErrInvalid))
	_, err = RemoveParts(before, []string{"#spec"})
	assert.True(t, errors.Is(err, ErrInvalid))
}

func TestRemovingASignedAppDropsWhatOnlyItsSignatureProtected(t *testing.T) {
	before := signedState(t)
	after, err := RemoveParts(before, []string{"nginx"})
	require.NoError(t, err)

	assert.NotContains(t, after, "_sigs/nginx.json")
	assert.NotContains(t, after, "nginx/root.squashfs")
	// _config/nginx went with the app, but _sigs/nginx_config.json still
	// protects it: the device would refuse that, and the plan says so.
	warnings := SignatureWarnings(before, after)
	require.NotEmpty(t, warnings)
	assert.Contains(t, warnings[0], "_sigs/nginx_config.json")
}

func TestCopyPartsReplacesThePartAndBringsItsSignature(t *testing.T) {
	dst := plainState()
	src := signedState(t)

	after, err := CopyParts(dst, src, []string{"nginx"})
	require.NoError(t, err)
	for _, key := range []string{"nginx/root.squashfs", "nginx/run.json", "_sigs/nginx.json", "_config/nginx/etc/nginx/nginx.conf"} {
		assert.Equal(t, src[key] != nil, after[key] != nil, key)
	}
	assert.Contains(t, after, "web/run.json", "other parts stay")
	assert.Empty(t, SignatureWarnings(dst, after), "the signature came with the files it protects")

	// Copying over an existing part drops the files the source no longer has.
	src2 := plainState()
	delete(src2, "web/old.bin")
	src2["web/run.json"] = map[string]interface{}{"name": "web", "v": 2.0}
	after, err = CopyParts(plainState(), src2, []string{"web"})
	require.NoError(t, err)
	assert.NotContains(t, after, "web/old.bin")
	assert.Equal(t, 2.0, after["web/run.json"].(map[string]interface{})["v"])

	_, err = CopyParts(dst, src, []string{"missing"})
	assert.True(t, errors.Is(err, ErrInvalid))
}

func TestImportMergesAnExportLikePvtx(t *testing.T) {
	dst := plainState()
	export := signedState(t)
	delete(export, "#spec")

	after, err := Import(dst, export)
	require.NoError(t, err)
	assert.Contains(t, after, "_sigs/nginx.json")
	assert.Contains(t, after, "nginx/root.squashfs")
	assert.Contains(t, after, "web/root.squashfs", "parts the export does not bring stay")
	assert.Equal(t, "pantavisor-service-system@1", after["#spec"])
	require.NoError(t, Validate(after))
}

func TestEditingASignedDocumentIsFlagged(t *testing.T) {
	before := signedState(t)

	after, err := SetDocument(before, "nginx/run.json", map[string]interface{}{"name": "nginx", "changed": true})
	require.NoError(t, err)
	warnings := SignatureWarnings(before, after)
	require.Len(t, warnings, 1)
	assert.Contains(t, warnings[0], "_sigs/nginx.json")
	assert.Contains(t, warnings[0], "nginx/run.json")

	// An unsigned document changes without a word.
	plain := plainState()
	after, err = SetDocument(plain, "web/run.json", map[string]interface{}{"name": "web2"})
	require.NoError(t, err)
	assert.Empty(t, SignatureWarnings(plain, after))
	assert.Equal(t, Diff{Added: []string{}, Removed: []string{}, Changed: []string{"web/run.json"}}, Compare(plain, after))
}

func TestDocumentsOnly(t *testing.T) {
	state := plainState()
	for _, path := range []string{"web/root.squashfs", "#spec", "_sigs/web.json", ""} {
		_, err := SetDocument(state, path, map[string]interface{}{})
		assert.True(t, errors.Is(err, ErrInvalid), path)
	}
	_, err := SetDocument(state, "web/run.json", nil)
	assert.True(t, errors.Is(err, ErrInvalid))

	after, err := DeleteFile(state, "web/old.bin")
	require.NoError(t, err)
	assert.NotContains(t, after, "web/old.bin")
	_, err = DeleteFile(state, "_sigs/x.json")
	assert.True(t, errors.Is(err, ErrInvalid))
}

func TestValidate(t *testing.T) {
	require.NoError(t, Validate(plainState()))

	noSpec := plainState()
	delete(noSpec, "#spec")
	assert.Error(t, Validate(noSpec))

	badObject := plainState()
	badObject["web/root.squashfs"] = map[string]interface{}{}
	assert.Error(t, Validate(badObject))
}

func TestSignatureWarningsNameUnsignedPartsOnSignedDevices(t *testing.T) {
	before := signedState(t)
	after, err := CopyParts(before, plainState(), []string{"web"})
	require.NoError(t, err)
	warnings := SignatureWarnings(before, after)
	require.NotEmpty(t, warnings)
	assert.Contains(t, strings.Join(warnings, " "), "web")
}

// pvr excludes <app>/src.json from an app's signature, and --noconfig its
// configuration. Those files are left out on purpose: importing such an app
// into a signed state must not be reported as unsigned.
func TestFilesASignatureExcludesAreNotUnsigned(t *testing.T) {
	signed := signedState(t)

	// nginx_config is signed apart and covers _config/nginx/**; the nginx
	// signature excludes nginx/src.json, which is in the state already.
	before, err := RemoveParts(signed, []string{"nginx"})
	require.NoError(t, err)

	after, err := Import(before, signed)
	require.NoError(t, err)
	assert.Contains(t, after, "nginx/src.json")

	for _, warning := range SignatureWarnings(before, after) {
		assert.NotContains(t, warning, "no signature protects", "an excluded file is not an unsigned part: %s", warning)
	}

	// A part nothing signs at all is still reported.
	unsigned, err := SetDocument(after, "telemetry/run.json", map[string]interface{}{"name": "telemetry"})
	require.NoError(t, err)
	warnings := SignatureWarnings(after, unsigned)
	require.NotEmpty(t, warnings)
	assert.Contains(t, strings.Join(warnings, " "), "telemetry")
}
