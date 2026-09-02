// Copyright 2026 Pantacor Ltd.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//   http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
package utils

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"os"
	"testing"
)

// The derived-key path is the default in every deployment that does not set
// PANTAHUB_JWT_OBJECT_SECRET explicitly; it must never resolve to the shipped
// placeholder. Both calls run in one process because the key is cached with
// sync.Once — the explicit-override path is trivial passthrough.
func TestGetObjectTokenSecretDerivedFromJWTKey(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	os.Setenv(EnvPantahubJWTAuthSecret, base64.StdEncoding.EncodeToString(pemBytes))
	os.Unsetenv(EnvPantahubJWTObjectSecret)

	got := GetObjectTokenSecret()
	if len(got) != 32 {
		t.Fatalf("expected 32-byte derived key, got %d bytes", len(got))
	}
	if string(got) == defaultEnvs[EnvPantahubJWTObjectSecret] {
		t.Fatal("derived key must not equal the shipped placeholder")
	}
	if string(GetObjectTokenSecret()) != string(got) {
		t.Fatal("derived key must be stable within the process")
	}
}
