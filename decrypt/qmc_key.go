package decrypt

// qmc_key.go — QMC key derivation (Tencent TEA-CBC)
//
// Raw key bytes are Base64-encoded. Optionally prefixed by "QQMusic EncV2,Key:"
// which requires double TEA decryption before the regular derivation.
//
// Regular derivation:
//  1. Build a 16-byte TEA key: interleave simpleMakeKey(106,8) with first 8 bytes of raw key.
//  2. TEA-CBC decrypt the remaining bytes (from offset 8) with that key.
//  3. Return first8 || decryptedRest.

import (
	"encoding/base64"
	"errors"
	"math"
	"strings"
)

const (
	teaSaltLen = 2
	teaZeroLen = 7
)

var (
	qmcMixKey1 = []byte{0x33, 0x38, 0x36, 0x5A, 0x4A, 0x59, 0x21, 0x40, 0x23, 0x2A, 0x24, 0x25, 0x5E, 0x26, 0x29, 0x28}
	qmcMixKey2 = []byte{0x2A, 0x2A, 0x23, 0x21, 0x28, 0x23, 0x24, 0x25, 0x26, 0x5E, 0x61, 0x31, 0x63, 0x5A, 0x2C, 0x54}
)

// QmcDeriveKey derives the final decryption key from the raw (Base64-encoded) key bytes.
func QmcDeriveKey(rawEncoded []byte) ([]byte, error) {
	rawDec, err := lenientBase64Decode(string(rawEncoded))
	if err != nil {
		return nil, errors.New("qmc/key: base64 decode failed: " + err.Error())
	}
	if len(rawDec) < 16 {
		return nil, errors.New("qmc/key: key too short")
	}

	rawDec, err = decryptV2Key(rawDec)
	if err != nil {
		return nil, err
	}

	simpleKey := simpleMakeKey(106, 8)
	teaKey := make([]byte, 16)
	for i := 0; i < 8; i++ {
		teaKey[i<<1] = byte(simpleKey[i])
		teaKey[(i<<1)+1] = rawDec[i]
	}

	sub, err := decryptTencentTea(rawDec[8:], teaKey)
	if err != nil {
		return nil, err
	}

	return append(rawDec[:8], sub...), nil
}

// simpleMakeKey generates an 8-byte key using tan() as a PRNG.
func simpleMakeKey(salt int, length int) []int {
	key := make([]int, length)
	for i := 0; i < length; i++ {
		tmp := math.Tan(float64(salt) + float64(i)*0.1)
		key[i] = int(math.Abs(tmp)*100.0) & 0xFF
	}
	return key
}

// decryptV2Key handles the optional "QQMusic EncV2,Key:" prefix.
func decryptV2Key(key []byte) ([]byte, error) {
	const prefix = "QQMusic EncV2,Key:"
	if len(key) < 18 || string(key[:18]) != prefix {
		return key, nil // not EncV2
	}

	out, err := decryptTencentTea(key[18:], qmcMixKey1)
	if err != nil {
		return nil, errors.New("qmc/key: EncV2 mixKey1 decrypt failed: " + err.Error())
	}
	out, err = decryptTencentTea(out, qmcMixKey2)
	if err != nil {
		return nil, errors.New("qmc/key: EncV2 mixKey2 decrypt failed: " + err.Error())
	}

	decoded, err := lenientBase64Decode(string(out))
	if err != nil {
		return nil, errors.New("qmc/key: EncV2 final base64 decode failed: " + err.Error())
	}
	if len(decoded) < 16 {
		return nil, errors.New("qmc/key: EncV2 decoded key too short")
	}
	return decoded, nil
}

// decryptTencentTea decrypts a Tencent custom TEA-CBC stream.
//
// Cipher format: PadLen(1B) | Padding(0-7B) | Salt(2B) | Body | Zero(7B)
// Rounds: 32 (not the standard 64).
func decryptTencentTea(inBuf, key []byte) ([]byte, error) {
	if len(inBuf)%8 != 0 {
		return nil, errors.New("tea: input size not multiple of block size")
	}
	if len(inBuf) < 16 {
		return nil, errors.New("tea: input too small")
	}

	tea, err := NewTeaCipher(key, 32)
	if err != nil {
		return nil, err
	}

	tmpBuf := make([]byte, 8)

	// Decrypt first block
	tea.Decrypt(tmpBuf, inBuf[:8])

	nPadLen := int(tmpBuf[0] & 0x7) // low 3 bits
	outLen := len(inBuf) - 1 - nPadLen - teaSaltLen - teaZeroLen
	if outLen < 0 {
		return nil, errors.New("tea: computed output length is negative")
	}
	outBuf := make([]byte, outLen)

	ivPrev := make([]byte, 8) // TypeScript: new Uint8Array(8) — must be zero-initialised, not nil
	ivCur := inBuf[:8]
	inBufPos := 8
	tmpIdx := 1 + nPadLen // skip PadLen byte and padding

	cryptBlock := func() {
		ivPrev = ivCur
		ivCur = inBuf[inBufPos : inBufPos+8]
		for j := 0; j < 8; j++ {
			tmpBuf[j] ^= ivCur[j]
		}
		tea.Decrypt(tmpBuf, tmpBuf)
		inBufPos += 8
		tmpIdx = 0
	}

	// Skip Salt
	for i := 1; i <= teaSaltLen; {
		if tmpIdx < 8 {
			tmpIdx++
			i++
		} else {
			cryptBlock()
		}
	}

	// Recover plaintext
	outBufPos := 0
	for outBufPos < outLen {
		if tmpIdx < 8 {
			outBuf[outBufPos] = tmpBuf[tmpIdx] ^ ivPrev[tmpIdx]
			outBufPos++
			tmpIdx++
		} else {
			cryptBlock()
		}
	}

	// Verify zero padding.
	// TypeScript does NOT increment tmpIdx here — it checks the same position teaZeroLen times.
	// This matches the original behaviour exactly (one-byte integrity check, repeated).
	for i := 1; i <= teaZeroLen; i++ {
		if tmpBuf[tmpIdx] != ivPrev[tmpIdx] {
			return nil, errors.New("tea: zero check failed")
		}
	}

	return outBuf, nil
}

// lenientBase64Decode mimics Node.js's Buffer.from(str, 'base64') behaviour:
//   - Strips whitespace and any character that is not a valid base64 symbol.
//   - Accepts both standard (+/) and URL-safe (-_) alphabets by normalising to standard first.
//   - Does not require padding; adds it automatically when needed.
func lenientBase64Decode(s string) ([]byte, error) {
	// 1. Normalise URL-safe to standard alphabet.
	s = strings.NewReplacer("-", "+", "_", "/").Replace(s)

	// 2. Strip every character that cannot appear in standard base64.
	var b strings.Builder
	b.Grow(len(s))
	for _, c := range s {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '+' || c == '/' || c == '=' {
			b.WriteRune(c)
		}
	}
	clean := b.String()

	// 3. Add missing padding so the length is a multiple of 4.
	switch len(clean) % 4 {
	case 2:
		clean += "=="
	case 3:
		clean += "="
	}

	return base64.StdEncoding.DecodeString(clean)
}
