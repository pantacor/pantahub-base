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
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"encoding/xml"
	"net/http"

	"github.com/tiaguinho/gosoap"
	"gitlab.com/pantacor/pantahub-base/utils"
)

// WsdlTP wsdl transport protocol
type WsdlTP struct {
	client *gosoap.Client
}

type certificateRequestResponse struct {
	XMLName xml.Name  `xml:"certificateRequestResponse"`
	Return  returnXML `xml:"return"`
}

type returnXML struct {
	XMLName xml.Name `xml:"return"`
	Cert    []byte   `xml:"data"`
	Type    string   `xml:"responseType"`
}

// WSDL create new WSDL transport protocol
func WSDL(URL string) (*WsdlTP, error) {
	caCert, err := base64.StdEncoding.DecodeString(utils.GetEnv(utils.EnvPantahubCaCert))
	if err != nil {
		return nil, err
	}

	p12Cert, err := base64.StdEncoding.DecodeString(utils.GetEnv(utils.EnvPantahubCaP12Cert))
	if err != nil {
		return nil, err
	}

	p12Key, err := base64.StdEncoding.DecodeString(utils.GetEnv(utils.EnvPantahubCaP12Key))
	if err != nil {
		return nil, err
	}

	clientCert, err := tls.X509KeyPair(p12Cert, p12Key)
	if err != nil {
		return nil, err
	}

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(caCert)

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:            pool,
				Certificates:       []tls.Certificate{clientCert},
				InsecureSkipVerify: true,
			},
		},
	}
	client, err := gosoap.SoapClient(URL, httpClient)
	if err != nil {
		return nil, err
	}

	return &WsdlTP{
		client: client,
	}, nil
}

// RequestCertificate for webservice interface
func (w *WsdlTP) RequestCertificate(cert *x509.CertificateRequest, deviceID string, secret string) ([]byte, error) {
	pem := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: cert.Raw})
	username := deviceID + "@pantahub.com"
	subject := "SERIALNUMBER=" + deviceID + ",CN=" + username + ",OU=PantahubDevices,O=PantacorLtd"

	params := gosoap.Params{
		"arg0": gosoap.Params{
			"username":               username,
			"password":               secret,
			"subjectDN":              subject,
			"caName":                 "PantacorCA",
			"clearPwd":               "false",
			"endEntityProfileName":   "pantahub",
			"certificateProfileName": "IDEVID",
			"keyRecoverable":         "false",
			"sendNotification":       "false",
			"status":                 "0",
		},
		"arg1": string(pem),
		"arg2": "0",
		"arg4": "CERTIFICATE",
	}
	res, err := w.client.Call("certificateRequest", params)
	if err != nil {
		return nil, err
	}
	response := &certificateRequestResponse{}
	err = res.Unmarshal(response)
	if err != nil {
		return nil, err
	}
	derBase64, err := base64.StdEncoding.DecodeString(string(response.Return.Cert))
	return derBase64, err
}
