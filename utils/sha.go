package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	cjson "github.com/gibson042/canonicaljson-go"
)

func DecodeSha256HexString(hexSha string) (sha []byte, err error) {
	sha, err = hex.DecodeString(hexSha)

	if err == nil && len(sha) != sha256.Size {
		err = errors.New("sha does not match expected length")
	}
	return
}

func StateSha(obj interface{}) (string, error) {
	json, err := cjson.Marshal(obj)

	if err != nil {
		return "", err
	}

	sha := hex.EncodeToString(sha256.New().Sum(json))
	return sha, err
}
