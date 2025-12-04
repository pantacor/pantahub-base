package decoder

import (
	"encoding/json"
	"errors"
	"io"

	"github.com/ant0ine/go-json-rest/rest"
	"gitlab.com/pantacor/pantahub-base/utils"
)

var ErrJsonPayloadEmpty = errors.New("JSON payload is empty")

func DecodeJsonPayload(r *rest.Request, v interface{}) error {
	content, err := io.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		return err
	}

	if len(content) == 0 {
		return ErrJsonPayloadEmpty
	}

	err = json.Unmarshal([]byte(utils.QuoteDollar(string(content))), v)
	if err != nil {
		return err
	}

	return nil
}
