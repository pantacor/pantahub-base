package utils

import "testing"

func TestVerifyStoredSecret(t *testing.T) {
	h := HashSecret("s3cr3t")

	// hashed row: match by hash, never upgrade
	if ok, up := VerifyStoredSecret(h, "", "s3cr3t"); !ok || up != "" {
		t.Fatalf("hashed match: ok=%v up=%q", ok, up)
	}
	if ok, _ := VerifyStoredSecret(h, "", "wrong"); ok {
		t.Fatal("hashed row must reject a wrong secret")
	}
	// hash present alongside legacy plaintext: hash wins, no plaintext fallback
	if ok, _ := VerifyStoredSecret(h, "other-plain", "other-plain"); ok {
		t.Fatal("plaintext must be ignored once a hash exists")
	}

	// legacy plaintext-only row: match and report the hash to persist
	ok, up := VerifyStoredSecret("", "s3cr3t", "s3cr3t")
	if !ok || up != h {
		t.Fatalf("plaintext fallback: ok=%v up=%q want hash", ok, up)
	}
	if ok, _ := VerifyStoredSecret("", "s3cr3t", "wrong"); ok {
		t.Fatal("plaintext row must reject a wrong secret")
	}

	// empty inputs never match
	for _, c := range [][3]string{{"", "", "x"}, {h, "", ""}, {"", "s", ""}} {
		if ok, _ := VerifyStoredSecret(c[0], c[1], c[2]); ok {
			t.Fatalf("empty input matched: %v", c)
		}
	}
}
