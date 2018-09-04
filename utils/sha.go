package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"

	cjson "github.com/gibson042/canonicaljson-go"
)

func DecodeSha256HexString(shaString string) (sha []byte, err error) {
	sha, err = hex.DecodeString(shaString)

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

	shaHash := sha256.New()

	_, err = shaHash.Write(json)

	if err != nil {
		return "", err
	}

	arr := make([]byte, sha256.Size)
	arr = shaHash.Sum(arr[:0])
	sha := hex.EncodeToString(arr)
	return sha, err
}
