package decrypt

// TeaCipher is a Go port of the TypeScript TEA implementation used for QQ Music key decryption.
// Reference: https://go.dev/src/golang.org/x/crypto/tea
// TEA block size is 8 bytes, key size is 16 bytes.
//
// NOTE: TEA is a legacy cipher; it is used here only for compatibility with QQ Music.

import (
	"encoding/binary"
	"errors"
)

const (
	teaBlockSize = 8
	teaKeySize   = 16
	teaDelta     = uint32(0x9E3779B9)
)

// TeaCipher holds the key schedule for a TEA cipher.
type TeaCipher struct {
	k0, k1, k2, k3 uint32
	rounds         int
}

// NewTeaCipher creates a new TEA cipher with the given key and number of rounds.
// rounds must be even; the standard is 64.
func NewTeaCipher(key []byte, rounds int) (*TeaCipher, error) {
	if len(key) != teaKeySize {
		return nil, errors.New("tea: incorrect key size")
	}
	if rounds%2 != 0 {
		return nil, errors.New("tea: odd number of rounds")
	}
	return &TeaCipher{
		k0:     binary.BigEndian.Uint32(key[0:4]),
		k1:     binary.BigEndian.Uint32(key[4:8]),
		k2:     binary.BigEndian.Uint32(key[8:12]),
		k3:     binary.BigEndian.Uint32(key[12:16]),
		rounds: rounds,
	}, nil
}

// Decrypt decrypts a single 8-byte block from src into dst.
func (t *TeaCipher) Decrypt(dst, src []byte) {
	v0 := binary.BigEndian.Uint32(src[0:4])
	v1 := binary.BigEndian.Uint32(src[4:8])

	// sum = delta * (rounds/2), computed in uint32 (wraps naturally)
	sum := teaDelta * uint32(t.rounds/2)

	for i := 0; i < t.rounds/2; i++ {
		v1 -= ((v0 << 4) + t.k2) ^ (v0 + sum) ^ ((v0 >> 5) + t.k3)
		v0 -= ((v1 << 4) + t.k0) ^ (v1 + sum) ^ ((v1 >> 5) + t.k1)
		sum -= teaDelta
	}

	binary.BigEndian.PutUint32(dst[0:4], v0)
	binary.BigEndian.PutUint32(dst[4:8], v1)
}
