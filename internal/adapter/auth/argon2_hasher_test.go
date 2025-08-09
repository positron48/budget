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


