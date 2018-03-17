package utils

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

func DecodeSha256HexString(hexSha string) (sha []byte, err error) {

	sha, err = hex.DecodeString(hexSha)

	if err == nil && len(sha) != sha256.Size {
		err = errors.New("sha does not match expected length")
	}
	return
}
