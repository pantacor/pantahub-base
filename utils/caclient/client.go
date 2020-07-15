// Copyright (c) 2019  Pantacor Ltd.
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

package caclient

import (
	"crypto/x509"
	"fmt"
)

// CertRequest send a certificate request to and CA
func CertRequest(csr *x509.CertificateRequest, username, password, caURL string, protocol TPType) ([]byte, error) {
	var err error = nil
	var cert []byte

	switch protocol {
	case TPWsdl:
		client, err := WSDL(caURL)
		if err != nil {
			return nil, err
		}

		cert, err = client.RequestCertificate(csr, username, password)
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("Protocol not implmented: %s", protocol)
	}

	return cert, err
}
