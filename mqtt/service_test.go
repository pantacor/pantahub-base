//
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

package mqtt

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeSelfSigned writes a self-signed cert/key pair for cn to the given paths.
func writeSelfSigned(t *testing.T, certPath, keyPath, cn string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		DNSNames:     []string{cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestTCPTLSConfigDisabledWhenUnset keeps the native listener plain when no
// cert is configured.
func TestTCPTLSConfigDisabledWhenUnset(t *testing.T) {
	t.Setenv(EnvMqttTLSCert, "")
	t.Setenv(EnvMqttTLSKey, "")
	cfg, err := tcpTLSConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg != nil {
		t.Fatal("expected nil TLS config when no cert is configured")
	}
}

// TestTCPTLSConfigRejectsHalfConfig is the guard against a silently
// unencrypted MQTTS port: a cert without a key (or vice versa) must fail
// startup, not fall back to plain MQTT.
func TestTCPTLSConfigRejectsHalfConfig(t *testing.T) {
	t.Setenv(EnvMqttTLSCert, "/some/cert.pem")
	t.Setenv(EnvMqttTLSKey, "")
	if _, err := tcpTLSConfig(); err == nil {
		t.Fatal("expected an error when only the cert is set")
	}

	t.Setenv(EnvMqttTLSCert, "")
	t.Setenv(EnvMqttTLSKey, "/some/key.pem")
	if _, err := tcpTLSConfig(); err == nil {
		t.Fatal("expected an error when only the key is set")
	}
}

// TestTCPTLSConfigLoadsCert builds a real TLS config from a cert on disk.
func TestTCPTLSConfigLoadsCert(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")
	writeSelfSigned(t, certPath, keyPath, "api.stage.pantahub.com")

	t.Setenv(EnvMqttTLSCert, certPath)
	t.Setenv(EnvMqttTLSKey, keyPath)

	cfg, err := tcpTLSConfig()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil || cfg.GetCertificate == nil {
		t.Fatal("expected a TLS config with a certificate resolver")
	}
	if _, err := cfg.GetCertificate(nil); err != nil {
		t.Fatalf("GetCertificate failed: %v", err)
	}
}

// TestCertReloaderPicksUpRotation is the reason for reloading: cert-manager
// rewrites the mounted secret in place, and the broker must serve the new
// certificate without a restart.
func TestCertReloaderPicksUpRotation(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "tls.crt")
	keyPath := filepath.Join(dir, "tls.key")
	writeSelfSigned(t, certPath, keyPath, "first.example")

	r := &certReloader{certPath: certPath, keyPath: keyPath}
	first, err := r.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(first.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if leaf.Subject.CommonName != "first.example" {
		t.Fatalf("got CN %q, want first.example", leaf.Subject.CommonName)
	}

	// Rotate the files in place with a strictly later modification time.
	time.Sleep(10 * time.Millisecond)
	writeSelfSigned(t, certPath, keyPath, "second.example")
	future := time.Now().Add(time.Second)
	if err := os.Chtimes(certPath, future, future); err != nil {
		t.Fatal(err)
	}

	second, err := r.GetCertificate(nil)
	if err != nil {
		t.Fatal(err)
	}
	leaf2, err := x509.ParseCertificate(second.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	if leaf2.Subject.CommonName != "second.example" {
		t.Fatalf("reloader served CN %q, want second.example after rotation", leaf2.Subject.CommonName)
	}
}
