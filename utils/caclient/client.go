// Copyright (c) 2020  Pantacor Ltd.
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

	"gitlab.com/pantacor/pantahub-base/utils"
)

// CAClient CA client
type CAClient struct {
	Protocol TPType
	URL      string
	Client   TCom
}

var caclient *CAClient = nil

// GetDefaultCAClient get singleton for caclient
func GetDefaultCAClient() (*CAClient, error) {
	if caclient != nil {
		return caclient, nil
	}

	var err error
	caclient, err = New(utils.GetEnv(utils.EnvPantahubCaServiceURL), TPWsdl)
	return caclient, err
}

// New get or create CAClient
func New(URL string, protocol TPType) (*CAClient, error) {
	client, err := getClient(URL, protocol)
	if err != nil {
		return nil, err
	}

	return &CAClient{
		Protocol: protocol,
		URL:      URL,
		Client:   client,
	}, nil
}

func getClient(URL string, protocol TPType) (TCom, error) {
	switch protocol {
	case TPWsdl:
		client, err := WSDL(URL)
		if err != nil {
			return nil, err
		}
		return client, nil
	default:
		return nil, fmt.Errorf("Protocol not implmented: %s", protocol)
	}
}

// CertRequest send a certificate request to and CA
func (ca *CAClient) CertRequest(csr *x509.CertificateRequest, username, password string) ([]byte, error) {
	return ca.Client.RequestCertificate(csr, username, password)
}
