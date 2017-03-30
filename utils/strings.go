//
// Copyright 2017  Alexander Sack <asac129@gmail.com>
//
package utils

import (
	"math/rand"
	"strings"
	"time"

	"github.com/asaskevich/govalidator"
)

// XXX: make this a nice prn helper tool
func PrnGetId(prn string) string {
	idx := strings.Index(prn, "/")
	return prn[idx+1 : len(prn)]
}

func IsNick(nick string) bool {
	l := len(nick)
	if l > 3 && l < 24 {
		return true
	}
	return false
}

func IsEmail(email string) bool {
	return govalidator.IsEmail(email)
}

var r *rand.Rand = rand.New(rand.NewSource(time.Now().UnixNano()))

func GenerateChallenge() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"

	result := make([]byte, 15)
	for i := range result {
		result[i] = chars[r.Intn(len(chars))]
	}

	return string(result)
}
