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

// ClientError Ca client error
type ClientError struct {
	err  string
	Code int
}

const (
	// ErrorNotConfig client configuration not found
	ErrorNotConfig = 0001

	// ErrorDecodingVariable error decoding base64 variable from environment
	ErrorDecodingVariable = 1000

	// ErrorParsingP12 error parsing P12 certificate
	ErrorParsingP12 = 1001

	// ErrorLoadingP12 error loading P12 certificate
	ErrorLoadingP12 = 1002

	// ErrorParsingCaCert error parsing ca certificate
	ErrorParsingCaCert = 1003

	// ErrorLoadingCaCert error loading ca certificate
	ErrorLoadingCaCert = 1004

	// ErrorLoadingSoap error loading soap client
	ErrorLoadingSoap = 1005
)

// NewError create a new error for ca client
func NewError(err string, code int) *ClientError {
	return &ClientError{
		err:  err,
		Code: code,
	}
}

func (e *ClientError) Error() string {
	return e.err
}
