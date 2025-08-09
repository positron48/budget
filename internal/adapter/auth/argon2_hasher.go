package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

type Argon2Params struct {
	Time    uint32
	Memory  uint32
	Threads uint8
	KeyLen  uint32
}

type Argon2Hasher struct {
	p Argon2Params
}

func NewArgon2Hasher() *Argon2Hasher {
	return &Argon2Hasher{p: Argon2Params{Time: 1, Memory: 64 * 1024, Threads: 4, KeyLen: 32}}
}

func (h *Argon2Hasher) Hash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, h.p.Time, h.p.Memory, h.p.Threads, h.p.KeyLen)
	// format: argon2id$v=19$m=65536,t=1,p=4$<salt>$<sum>
	return fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", h.p.Memory, h.p.Time, h.p.Threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func (h *Argon2Hasher) Verify(hash, password string) bool {
	parts := strings.Split(hash, "$")
	// expected: ["argon2id", "v=19", "m=...,t=...,p=...", "<salt>", "<sum>"]
	if len(parts) != 5 {
		return false
	}
	if parts[0] != "argon2id" || parts[1] != "v=19" {
		return false
	}
	var m uint32
	var t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[2], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	sum, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	key := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(sum)))
	return subtle.ConstantTimeCompare(sum, key) == 1
}
