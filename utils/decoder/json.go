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

package decoder

import (
	"encoding/json"
	"errors"
	"io"

	"gitlab.com/pantacor/pantahub-base/utils"
)

var ErrJsonPayloadEmpty = errors.New("JSON payload is empty")

// DecodeJsonBody reads and closes body, then decodes it with "$" keys quoted.
func DecodeJsonBody(body io.ReadCloser, v interface{}) error {
	content, err := io.ReadAll(body)
	_ = body.Close()
	if err != nil {
		return err
	}

	if len(content) == 0 {
		return ErrJsonPayloadEmpty
	}

	err = json.Unmarshal(utils.QuoteDollarBytes(content), v)
	if err != nil {
		return err
	}

	return nil
}
