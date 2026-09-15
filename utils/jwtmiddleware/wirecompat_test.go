package jwtmiddleware

import (
	"testing"

	jwtgo "github.com/dgrijalva/jwt-go"
)

// These tokens were minted by github.com/dgrijalva/jwt-go v3.2.0 -- the library
// this repo shipped before the move to golang-jwt/jwt/v5 -- and are frozen here
// deliberately.
//
// Devices and pvr clients hold long-lived tokens. If the v5 move changed how a
// token is parsed or validated in any way that matters, these fixtures stop
// verifying and this test fails, instead of the whole fleet silently failing to
// authenticate against a deployed build. Do not regenerate them: a fixture that
// is regenerated after a behaviour change tests nothing.
//
// Both were signed with fixtureKey using HS256. exp is year 2123 so they do not
// rot. Note the struct-claims token spells "aud" as a bare JSON string, which is
// what v3 emitted; v5 models Audience as ClaimStrings and must still accept it.
const (
	fixtureKey = "fixture-hmac-key-do-not-use-in-prod"

	// MapClaims, shaped like what LoginHandler mints.
	v3MapClaimsToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOiJwcm46cGFudGFodWIuY29tOmFwaXM6L2FwaSIsImV4cCI6NDg1MzYwMDAwMCwiaWQiOiJhZG1pbiIsIm5pY2siOiJhZG1pbiIsIm9yaWdfaWF0IjoxNzAwMDAwMDAwfQ.byLxgggxEL7Y_Fff1_0LjRrRvNBPEG3b1jf4RjpW5FQ"

	// Typed claims with an embedded StandardClaims (v5: RegisteredClaims).
	v3StructClaimsToken = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJ1c2VybmFtZSI6ImFkbWluIiwicHJuIjoicHJuOjo6YWNjb3VudHM6L2FkbWluIiwic2NvcGUiOiJhbGwiLCJtZXRob2RzIjpbInBhc3N3b3JkIl0sImF1ZCI6InBybjpwYW50YWh1Yi5jb206YXBpczovYXBpIiwiZXhwIjo0ODUzNjAwMDAwLCJqdGkiOiIwYmFkYzBmZmVlIiwiaWF0IjoxNzAwMDAwMDAwLCJzdWIiOiJwcm46OjphY2NvdW50czovYWRtaW4ifQ.hCDS57g6gmuaL2jOf71tKKbzwC0keQKUXdivPw98k7s"
)

func keyFunc(*jwtgo.Token) (interface{}, error) { return []byte(fixtureKey), nil }

// A v3-minted MapClaims token must still parse, verify and validate.
func TestV3MapClaimsTokenStillVerifies(t *testing.T) {
	tok, err := jwtgo.Parse(v3MapClaimsToken, keyFunc)
	if err != nil {
		t.Fatalf("parsing a v3-minted token: %v", err)
	}
	if !tok.Valid {
		t.Fatal("token did not validate")
	}

	claims, ok := tok.Claims.(jwtgo.MapClaims)
	if !ok {
		t.Fatalf("claims are %T, want MapClaims", tok.Claims)
	}
	for k, want := range map[string]interface{}{
		"id":   "admin",
		"nick": "admin",
		"aud":  "prn:pantahub.com:apis:/api",
	} {
		if got := claims[k]; got != want {
			t.Errorf("claim %q = %v, want %v", k, got, want)
		}
	}
	// orig_iat drives RefreshHandler's "too old to refresh" arithmetic, and it
	// must keep decoding as a JSON number.
	if _, ok := claims["orig_iat"].(float64); !ok {
		t.Errorf("orig_iat is %T, want float64", claims["orig_iat"])
	}
}

// The typed-claims token is parsed as MapClaims here on purpose: this asserts
// the token BYTES, independent of which Go struct models them, so the test
// survives the StandardClaims -> RegisteredClaims rename.
func TestV3StructClaimsTokenStillVerifies(t *testing.T) {
	tok, err := jwtgo.Parse(v3StructClaimsToken, keyFunc)
	if err != nil {
		t.Fatalf("parsing a v3-minted struct-claims token: %v", err)
	}
	if !tok.Valid {
		t.Fatal("token did not validate")
	}

	claims := tok.Claims.(jwtgo.MapClaims)
	for k, want := range map[string]interface{}{
		"username": "admin",
		"prn":      "prn:::accounts:/admin",
		"scope":    "all",
		"jti":      "0badc0ffee",
		"sub":      "prn:::accounts:/admin",
		// v3 emitted a single audience as a bare string, not an array.
		"aud": "prn:pantahub.com:apis:/api",
	} {
		if got := claims[k]; got != want {
			t.Errorf("claim %q = %v, want %v", k, got, want)
		}
	}
}

// A token signed with the wrong key must be rejected. Guards against a keyfunc
// or validation regression that would accept anything.
func TestWrongKeyIsRejected(t *testing.T) {
	_, err := jwtgo.Parse(v3MapClaimsToken, func(*jwtgo.Token) (interface{}, error) {
		return []byte("a-completely-different-key"), nil
	})
	if err == nil {
		t.Fatal("a token signed with a different key was accepted")
	}
}
