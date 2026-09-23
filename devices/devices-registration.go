// Copyright (c) 2017-2026 Pantacor Ltd.
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
	"crypto/x509"
	"encoding/asn1"
	"encoding/base64"
	"net/http"

	"github.com/labstack/echo/v5"
	"gitlab.com/pantacor/pantahub-base/utils"
	"gitlab.com/pantacor/pantahub-base/utils/caclient"
	"gitlab.com/pantacor/pantahub-base/utils/echoutil"
	"go.mongodb.org/mongo-driver/mongo"
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
func (a *App) handleRegister(c *echo.Context) error {
	ca, err := caclient.GetDefaultCAClient()
	if err != nil {
		return echoutil.RestErrorWrapper(c, "This feature is not available: "+err.Error(), http.StatusBadRequest)
	}

	reqPayload := &registerReq{}
	err = echoutil.DecodeJsonPayload(c, reqPayload)
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), http.StatusBadRequest)
	}

	certRaw, err := base64.StdEncoding.DecodeString(reqPayload.Cert)
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), http.StatusBadRequest)
	}

	cert, err := x509.ParseCertificateRequest(certRaw)
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), http.StatusBadRequest)
	}

	err = cert.CheckSignature()
	if err != nil {
		return echoutil.RestErrorWrapper(c, err.Error(), http.StatusBadRequest)
	}

	extensions := ProcessPHExtentions(cert)

	col := a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices_tokens")
	err = utils.ValidateOwnerSig(
		c.Request().Context(),
		base64.StdEncoding.EncodeToString([]byte(extensions.NameSigByOwner)),
		extensions.TokenID,
		extensions.Owner,
		reqPayload.Name,
		col,
	)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Invalid signature: "+err.Error(), http.StatusBadRequest)
	}

	// Check device quota before creating device
	quotaResult, err := CheckDeviceQuota(c.Request().Context(), extensions.Owner, a.mongoClient, a.subService)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error checking device quota: "+err.Error(), http.StatusInternalServerError)
	}
	if quotaResult.Exceeded {
		return echoutil.RestErrorWrapperUser(c, "device quota exceeded",
			"Device quota exceeded; delete some devices or request a quota bump from team@pantahub.com",
			http.StatusForbidden)
	}

	secret := base64.RawStdEncoding.EncodeToString([]byte(extensions.NameSigByOwner))
	device, err := createDevice(reqPayload.DeviceName, secret, extensions.Owner)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Error creating device: "+err.Error(), http.StatusBadRequest)
	}

	finalCert, err := ca.CertRequest(cert, device.ID.Hex(), secret)
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Failed to generate certificate on CA:"+err.Error(), http.StatusBadRequest)
	}

	// Create or update device with the new certificate
	device.DeviceMeta["idevid"] = finalCert
	_, err = device.save(c.Request().Context(), a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices"))
	if mongo.IsDuplicateKeyError(err) {
		// the id exists under another owner; the owner-bound upsert refused it
		return echoutil.RestErrorWrapper(c, "Device id already in use", http.StatusConflict)
	}
	if err != nil {
		return echoutil.RestErrorWrapper(c, "Failed to save device:"+err.Error(), http.StatusBadRequest)
	}

	response := &registerRes{
		Cert:   string(finalCert),
		Device: device,
	}
	return echoutil.WriteJSON(c, http.StatusOK, response)
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
