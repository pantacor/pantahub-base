// Copyright 2026 Pantacor Ltd.
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
	"errors"
	"path"
	"path/filepath"
	"strings"
)

// PantahubS3Path get S3 pantahub path from environment
func PantahubS3Path() string {
	if GetEnv(EnvPantahubStorageDriver) == "s3" {
		return GetEnv(EnvPantahubStoragePath)
	}

	basePath := path.Join(GetEnv(EnvPantahubS3Path), GetEnv(EnvPantahubStoragePath))

	if basePath == "" {
		basePath = "."
	}

	return basePath
}

// MakeLocalS3PathForName create a local S3 path for name.
//
// The returned path is always inside the storage base directory. Cleaning the
// name on its own was not enough for that: path.Clean leaves a leading "..",
// and filepath.Join then resolves it, so a name of "../../etc/passwd" used to
// produce a path outside the base. Callers happen to pass server-issued
// identifiers today, but containment is this function's job, not theirs.
func MakeLocalS3PathForName(name string) (string, error) {
	if filepath.Separator != '/' && strings.ContainsRune(name, filepath.Separator) ||
		strings.Contains(name, "\x00") {
		return "", errors.New("http: invalid character in file path")
	}

	basePath := PantahubS3Path()

	// Anchoring to "/" before cleaning drops any leading "..", the same way a
	// static file server resolves a request path against its root.
	anchored := path.Clean("/" + strings.ReplaceAll(name, "\\", "/"))
	fullPath := filepath.Join(basePath, filepath.FromSlash(anchored))

	// Belt and braces: confirm the result really is under the base, so a future
	// change to the cleaning above cannot quietly reintroduce an escape.
	relative, err := filepath.Rel(basePath, fullPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", errors.New("http: file path escapes storage directory")
	}

	return fullPath, nil
}
