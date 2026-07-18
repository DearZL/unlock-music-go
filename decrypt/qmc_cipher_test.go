package decrypt

import (
	"bytes"
	"testing"
)

func TestQmcRC4FirstSegmentUsesUnsignedKeyIndex(t *testing.T) {
	key := make([]byte, 512)
	key[0] = 0x44
	cipher := &QmcRC4Cipher{
		key:  key,
		n:    len(key),
		hash: 0x99773520,
	}

	// CommonFunction.dll calculates this as
	// floor(0x99773520 / (1 * 0x44) * 100) % 512 == 32.
	// Keeping the intermediate value unsigned avoids a 32-bit Go int overflow.
	if got := cipher.getFirstSegmentKey(1); got != 32 {
		t.Fatalf("first RC4 segment key index = %d, want 32", got)
	}
}

func TestQmcRC4ChunkedAndWholeStreamMatch(t *testing.T) {
	key := make([]byte, 512)
	for i := range key {
		key[i] = byte(i + 1)
	}
	source := make([]byte, 13_000)
	for i := range source {
		source[i] = byte(i*29 + 7)
	}

	whole := append([]byte(nil), source...)
	NewQmcRC4Cipher(key).Decrypt(whole, 0)

	chunked := append([]byte(nil), source...)
	for start := 0; start < len(chunked); {
		end := start + 733
		if end > len(chunked) {
			end = len(chunked)
		}
		NewQmcRC4Cipher(key).Decrypt(chunked[start:end], start)
		start = end
	}

	if !bytes.Equal(chunked, whole) {
		t.Fatal("chunked QMC RC4 output differs from whole-stream output")
	}
}
