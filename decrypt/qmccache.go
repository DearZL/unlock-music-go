package decrypt

// qmccache.go — QQ Music cache (.cache) decryption
//
// Each byte is:
//   1. XOR'd with 0xF4
//   2. Rotated left by 2 bits: ((b & 0x3F) << 2) | (b >> 6)

// DecryptQmcCache decrypts a QQ Music cache file and returns the plaintext audio bytes.
func DecryptQmcCache(data []byte) []byte {
	out := make([]byte, len(data))
	for i, b := range data {
		b ^= 0xF4
		out[i] = ((b & 0x3F) << 2) | (b >> 6)
	}
	return out
}
