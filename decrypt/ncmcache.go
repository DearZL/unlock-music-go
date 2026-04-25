package decrypt

// ncmcache.go — Netease Cloud Music cache (.uc) decryption
//
// The cache format simply XORs every byte with 0xA3 (163 decimal).

// DecryptNcmCache decrypts a Netease cache file in-place and returns the result.
// The decryption is symmetrical: applying it twice restores the original data.
func DecryptNcmCache(data []byte) []byte {
	out := make([]byte, len(data))
	for i, b := range data {
		out[i] = b ^ 0xA3
	}
	return out
}
