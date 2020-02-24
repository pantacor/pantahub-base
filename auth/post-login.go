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

// Package auth package to manage extensions of the oauth protocol
package auth

import (
	"errors"

	"github.com/cloudflare/cfssl/revoke"
	"gitlab.com/pantacor/pantahub-base/devices"
	"gitlab.com/pantacor/pantahub-base/utils"
)

func authenticateUsingCert(certPemBase64 string, deviceID string) (bool, error) {
	cert, err := utils.ParseBase64PemCert(certPemBase64)
	if err != nil {
		return false, nil
	}

	err = utils.ValidateCaSigned(cert)
	if err != nil {
		return true, errors.New("The certificate is can't be trusted")
	}

	revoked, ok := revoke.VerifyCertificate(cert)
	if revoked && !ok {
		return true, errors.New("The certificate is not valid anymore, could be revoked or is expired")
	}

	return cert.Subject.SerialNumber == deviceID, nil
}

func (a *App) getDevicePrnFromCertificate(certPemBase64 string) (string, error) {
	cert, err := utils.ParseBase64PemCert(certPemBase64)
	if err != nil {
		return "", err
	}

	deviceID := cert.Subject.SerialNumber

	device, err := devices.GetDeviceByID(deviceID, a.mongoClient.Database(utils.MongoDb).Collection("pantahub_devices"))
	return device.Prn, err
}
