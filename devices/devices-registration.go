// Copyright 2016-2018  Pantacor Ltd.
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

// Package devices all devices related logic
package devices

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/caclient"
)

type registerReq struct {
	Cert       string `json:"csr"`
	Name       string `json:"name"`
	DeviceName string `json:"device-name"`
}

type registerRes struct {
	Cert   string  `json:"crt"`
	Device *Device `json:"device"`
}

type issueReq struct{}

type issueRes struct{}

// PHCertExtensions all the indentifiers for pantahub extensions on a certificate struct
type PHCertExtensions struct {
	AIKName       asn1.ObjectIdentifier
	OwnerPrnOID   asn1.ObjectIdentifier
	OwnernameSig  asn1.ObjectIdentifier
	TokenID       asn1.ObjectIdentifier
	CertifyAttest asn1.ObjectIdentifier
	CertifySig    asn1.ObjectIdentifier
	QuoteAttest   asn1.ObjectIdentifier
	QuoteSig      asn1.ObjectIdentifier
	QuotePcrList  asn1.ObjectIdentifier
	DevicePRN     asn1.ObjectIdentifier
}

// PHExtensions pantacor certificate extensions
type PHExtensions struct {
	Owner            string
	TokenID          string
	NameSigByOwner   string
	QuotePcrList     []byte
	CertifyAttest    []byte
	CertifySignature []byte
	QuoteAttest      []byte
	QuoteSignature   []byte
}

var phCertExtensionIDs = &PHCertExtensions{
	AIKName:       asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 54621, 100, 0},
	OwnerPrnOID:   asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 54621, 100, 1},
	OwnernameSig:  asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 54621, 100, 2},
	TokenID:       asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 54621, 100, 3},
	CertifyAttest: asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 54621, 100, 4},
	CertifySig:    asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 54621, 100, 5},
	QuoteAttest:   asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 54621, 100, 6},
	QuoteSig:      asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 54621, 100, 7},
	QuotePcrList:  asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 54621, 100, 8},
	DevicePRN:     asn1.ObjectIdentifier{1, 3, 6, 1, 4, 1, 54621, 100, 9},
}

// handleRegister Register a new device using the IDevID csr
// @Summary Register a new device using the IDevID csr
// @Description Register a new device using the IDevID csr
// @Accept  json
// @Produce  json
// @Security ApiKeyAuth
// @Tags devices
// @Param body body registerReq true "Register Request"
// @Success 200 {object} Device
// @Failure 400 {object} utils.RError
// @Failure 404 {object} utils.RError
// @Failure 500 {object} utils.RError
// @Router /devices/register [post]
func (a *App) handleRegister(w rest.ResponseWriter, r *rest.Request) {
	reqPayload := &registerReq{}
	err := r.DecodeJsonPayload(reqPayload)
	if err != nil {
		rest.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	certRaw, err := base64.StdEncoding.DecodeString(reqPayload.Cert)
	if err != nil {
		rest.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	cert, err := x509.ParseCertificateRequest(certRaw)
	if err != nil {
		rest.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	err = cert.CheckSignature()
	if err != nil {
		rest.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	extensions := ProcessPHExtentions(cert)

	col := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	err = utils.ValidateOwnerSig(
		base64.StdEncoding.EncodeToString([]byte(extensions.NameSigByOwner)),
		extensions.TokenID,
		extensions.Owner,
		reqPayload.Name,
		col,
	)
	if err != nil {
		rest.Error(w, "Invalid signature: "+err.Error(), http.StatusBadRequest)
		return
	}

	secret := base64.RawStdEncoding.EncodeToString([]byte(extensions.NameSigByOwner))
	device, err := createDevice(reqPayload.DeviceName, secret, extensions.Owner)
	if err != nil {
		rest.Error(w, "Error creating device: "+err.Error(), http.StatusBadRequest)
		return
	}

	caURL := utils.GetEnv(utils.EnvPantahubCaServiceURL)
	finalCert, err := caclient.CertRequest(cert, device.ID.Hex(), secret, caURL, caclient.TPWsdl)
	if err != nil {
		rest.Error(w, "Failed to generate certificate on CA:"+err.Error(), http.StatusBadRequest)
		return
	}

	// Create or update device with the new certificate
	device.DeviceMeta["idevid"] = finalCert
	_, err = device.save(a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices"))
	if err != nil {
		rest.Error(w, "Failed to save device:"+err.Error(), http.StatusBadRequest)
		return
	}

	response := &registerRes{
		Cert:   string(finalCert),
		Device: device,
	}
	w.WriteJson(response)
}

func (a *App) handleIssueDeviceCert(w rest.ResponseWriter, r *rest.Request) {

}

func requestCertificate(deviceID string) ([]byte, error) {
	caHost := utils.GetEnv(utils.EnvPantahubCaHost)
	caPass := utils.GetEnv(utils.EnvPantahubCaRaPass)
	caUser := utils.GetEnv(utils.EnvPantahubCaRaUser)

	caCertPem, err := base64.StdEncoding.DecodeString(utils.GetEnv(utils.EnvPantahubCaCert))
	if err != nil {
		return nil, err
	}

	caCert, err := utils.CreateTmpFile(caCertPem, "server.pem", "")
	if err != nil {
		return nil, err
	}
	defer os.Remove(caCert)

	cert, err := utils.CreateTmpFile(nil, "cert", "")
	if err != nil {
		return nil, err
	}
	defer os.Remove(cert)

	privKey, err := utils.CreateTmpFile(nil, "privKey", "")
	if err != nil {
		return nil, err
	}
	defer os.Remove(privKey)

	r, err := run("openssl", "genrsa", "-out", privKey, "2048")
	if err != nil {
		return nil, errors.New(string(r))
	}

	r, err = run(
		"openssl",
		"cmp",
		"-cmd", "cr",
		"-path", "ejbca/publicweb/cmp/ra",
		"-srvcert", caCert,
		"-server", caHost,
		"-ref", caUser,
		"-secret", caPass,
		"-certout", cert,
		"-newkey", privKey,
		"-subject", buildSubject(deviceID),
	)
	if err != nil {
		return nil, errors.New(string(r))
	}

	data, err := ioutil.ReadFile(cert)
	if err != nil {
		return nil, err
	}

	return data, nil
}

func parsePrivateKey(der []byte) (crypto.PrivateKey, error) {
	if key, err := x509.ParsePKCS1PrivateKey(der); err == nil {
		return key, nil
	}

	if key, err := x509.ParsePKCS8PrivateKey(der); err == nil {
		switch key := key.(type) {
		case *rsa.PrivateKey, *ecdsa.PrivateKey:
			return key, nil
		default:
			return nil, fmt.Errorf("Found unknown private key type in PKCS#8 wrapping")
		}
	}

	if key, err := x509.ParseECPrivateKey(der); err == nil {
		return key, nil
	}

	return nil, fmt.Errorf("Failed to parse private key")
}

func buildSubject(deviceID string) string {
	return "/O=PantacorLtd/OU=PantahubDevices/CN=" + deviceID + "@aca.pantahub.com/serialNumber=" + deviceID
}

func run(command string, params ...string) ([]byte, error) {
	cmd := exec.Command(command, params...)
	response, err := cmd.CombinedOutput()
	return response, err
}

// ProcessPHExtentions process all pantacor extensions if they exists
func ProcessPHExtentions(cert *x509.CertificateRequest) *PHExtensions {
	extensions := &PHExtensions{}

	for _, ext := range cert.Extensions {
		switch id := ext.Id.String(); id {
		case phCertExtensionIDs.OwnernameSig.String():
			extensions.NameSigByOwner = string(ext.Value)

		case phCertExtensionIDs.OwnerPrnOID.String():
			extensions.Owner = string(ext.Value)

		case phCertExtensionIDs.CertifyAttest.String():
			extensions.CertifyAttest = ext.Value

		case phCertExtensionIDs.CertifySig.String():
			extensions.CertifySignature = ext.Value

		case phCertExtensionIDs.TokenID.String():
			extensions.TokenID = string(ext.Value)

		case phCertExtensionIDs.QuoteAttest.String():
			extensions.QuoteAttest = ext.Value

		case phCertExtensionIDs.QuoteSig.String():
			extensions.QuoteSignature = ext.Value

		case phCertExtensionIDs.QuotePcrList.String():
			extensions.QuotePcrList = ext.Value
		default:
		}
	}

	return extensions
}
