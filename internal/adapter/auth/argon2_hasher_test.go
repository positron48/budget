package auth

import (
	"regexp"
	"testing"
)

func TestArgon2Hasher_HashAndVerify(t *testing.T) {
	h := NewArgon2Hasher()
	hash, err := h.Hash("secret")
	if err != nil {
		t.Fatalf("hash error: %v", err)
	}
	re := regexp.MustCompile(`^argon2id\$v=19\$m=\d+,t=\d+,p=\d+\$[A-Za-z0-9_\-+/]+\$[A-Za-z0-9_\-+/]+$`)
	if !re.MatchString(hash) {
		t.Fatalf("unexpected hash format: %s", hash)
	}
	if !h.Verify(hash, "secret") {
		t.Fatalf("verify should succeed for correct password")
	}
	if h.Verify(hash, "wrong") {
		t.Fatalf("verify should fail for wrong password")
	}
}

func TestArgon2Hasher_InvalidHashes(t *testing.T) {
    h := NewArgon2Hasher()
    // wrong parts count
    if h.Verify("invalid$hash", "secret") {
        t.Fatal("verify should fail on malformed hash")
    }
    // wrong prefix/version
    bad := "argon2i$v=18$m=65536,t=1,p=4$AAAA$BBBB"
    if h.Verify(bad, "secret") {
        t.Fatal("verify should fail on wrong method/version")
    }
}
