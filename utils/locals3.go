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

// MakeLocalS3PathForName create a local S3 path for name
func MakeLocalS3PathForName(name string) (string, error) {
	if filepath.Separator != '/' && strings.ContainsRune(name, filepath.Separator) ||
		strings.Contains(name, "\x00") {
		return "", errors.New("http: invalid character in file path")
	}

	basePath := PantahubS3Path()

	return filepath.Join(basePath, filepath.FromSlash(path.Clean(name))), nil
}
