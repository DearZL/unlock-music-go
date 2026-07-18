package decrypt

// qmc_cipher.go — QMC stream cipher implementations
//
// Three cipher types are used by QQ Music:
//   StaticCipher  – fixed 256-byte substitution table, no key required
//   MapCipher     – XOR with per-position mask derived from a short key (< 300 bytes)
//   RC4Cipher     – modified RC4 with segment-based decryption, for long keys (≥ 300 bytes)

import "math"

// QmcStreamCipher is the common interface for all QMC ciphers.
type QmcStreamCipher interface {
	// Decrypt decrypts buf in-place. offset is the position of buf[0] within the stream.
	Decrypt(buf []byte, offset int)
}

// ──────────────────────────────────────────────────────────
// Static Cipher
// ──────────────────────────────────────────────────────────

var qmcStaticBox = [256]byte{
	0x77, 0x48, 0x32, 0x73, 0xDE, 0xF2, 0xC0, 0xC8,
	0x95, 0xEC, 0x30, 0xB2, 0x51, 0xC3, 0xE1, 0xA0,
	0x9E, 0xE6, 0x9D, 0xCF, 0xFA, 0x7F, 0x14, 0xD1,
	0xCE, 0xB8, 0xDC, 0xC3, 0x4A, 0x67, 0x93, 0xD6,
	0x28, 0xC2, 0x91, 0x70, 0xCA, 0x8D, 0xA2, 0xA4,
	0xF0, 0x08, 0x61, 0x90, 0x7E, 0x6F, 0xA2, 0xE0,
	0xEB, 0xAE, 0x3E, 0xB6, 0x67, 0xC7, 0x92, 0xF4,
	0x91, 0xB5, 0xF6, 0x6C, 0x5E, 0x84, 0x40, 0xF7,
	0xF3, 0x1B, 0x02, 0x7F, 0xD5, 0xAB, 0x41, 0x89,
	0x28, 0xF4, 0x25, 0xCC, 0x52, 0x11, 0xAD, 0x43,
	0x68, 0xA6, 0x41, 0x8B, 0x84, 0xB5, 0xFF, 0x2C,
	0x92, 0x4A, 0x26, 0xD8, 0x47, 0x6A, 0x7C, 0x95,
	0x61, 0xCC, 0xE6, 0xCB, 0xBB, 0x3F, 0x47, 0x58,
	0x89, 0x75, 0xC3, 0x75, 0xA1, 0xD9, 0xAF, 0xCC,
	0x08, 0x73, 0x17, 0xDC, 0xAA, 0x9A, 0xA2, 0x16,
	0x41, 0xD8, 0xA2, 0x06, 0xC6, 0x8B, 0xFC, 0x66,
	0x34, 0x9F, 0xCF, 0x18, 0x23, 0xA0, 0x0A, 0x74,
	0xE7, 0x2B, 0x27, 0x70, 0x92, 0xE9, 0xAF, 0x37,
	0xE6, 0x8C, 0xA7, 0xBC, 0x62, 0x65, 0x9C, 0xC2,
	0x08, 0xC9, 0x88, 0xB3, 0xF3, 0x43, 0xAC, 0x74,
	0x2C, 0x0F, 0xD4, 0xAF, 0xA1, 0xC3, 0x01, 0x64,
	0x95, 0x4E, 0x48, 0x9F, 0xF4, 0x35, 0x78, 0x95,
	0x7A, 0x39, 0xD6, 0x6A, 0xA0, 0x6D, 0x40, 0xE8,
	0x4F, 0xA8, 0xEF, 0x11, 0x1D, 0xF3, 0x1B, 0x3F,
	0x3F, 0x07, 0xDD, 0x6F, 0x5B, 0x19, 0x30, 0x19,
	0xFB, 0xEF, 0x0E, 0x37, 0xF0, 0x0E, 0xCD, 0x16,
	0x49, 0xFE, 0x53, 0x47, 0x13, 0x1A, 0xBD, 0xA4,
	0xF1, 0x40, 0x19, 0x60, 0x0E, 0xED, 0x68, 0x09,
	0x06, 0x5F, 0x4D, 0xCF, 0x3D, 0x1A, 0xFE, 0x20,
	0x77, 0xE4, 0xD9, 0xDA, 0xF9, 0xA4, 0x2B, 0x76,
	0x1C, 0x71, 0xDB, 0x00, 0xBC, 0xFD, 0x0C, 0x6C,
	0xA5, 0x47, 0xF7, 0xF6, 0x00, 0x79, 0x4A, 0x11,
}

// QmcStaticCipher uses a fixed 256-byte substitution table.
type QmcStaticCipher struct{}

func (c *QmcStaticCipher) getMask(offset int) byte {
	if offset > 0x7FFF {
		offset %= 0x7FFF
	}
	return qmcStaticBox[(offset*offset+27)&0xFF]
}

func (c *QmcStaticCipher) Decrypt(buf []byte, offset int) {
	for i := range buf {
		buf[i] ^= c.getMask(offset + i)
	}
}

// ──────────────────────────────────────────────────────────
// Map Cipher
// ──────────────────────────────────────────────────────────

// QmcMapCipher uses a key-derived per-position XOR mask.
type QmcMapCipher struct {
	key []byte
	n   int
}

// NewQmcMapCipher creates a MapCipher from key.
func NewQmcMapCipher(key []byte) *QmcMapCipher {
	if len(key) == 0 {
		panic("qmc/cipher_map: invalid key size")
	}
	return &QmcMapCipher{key: key, n: len(key)}
}

func mapRotate(value byte, bits int) byte {
	rotate := (bits + 4) % 8
	left := value << rotate
	right := value >> rotate
	return left | right
}

func (c *QmcMapCipher) getMask(offset int) byte {
	if offset > 0x7FFF {
		offset %= 0x7FFF
	}
	idx := (offset*offset + 71214) % c.n
	return mapRotate(c.key[idx], idx&0x7)
}

func (c *QmcMapCipher) Decrypt(buf []byte, offset int) {
	for i := range buf {
		buf[i] ^= c.getMask(offset + i)
	}
}

// ──────────────────────────────────────────────────────────
// RC4 Cipher
// ──────────────────────────────────────────────────────────

const (
	rc4FirstSegmentSize = 0x80
	rc4SegmentSize      = 5120 // 0x1400
)

// QmcRC4Cipher implements the modified RC4 used for long QMC keys.
type QmcRC4Cipher struct {
	key  []byte
	n    int
	s    []byte // permutation box (len = n)
	hash uint32 // pre-computed hash of key — must be uint32 (TypeScript uses >>> 0 truncation)
}

// NewQmcRC4Cipher creates an RC4Cipher from key.
func NewQmcRC4Cipher(key []byte) *QmcRC4Cipher {
	if len(key) == 0 {
		panic("qmc/cipher_rc4: invalid key size")
	}
	n := len(key)
	s := make([]byte, n)
	for i := range s {
		s[i] = byte(i)
	}
	j := 0
	for i := 0; i < n; i++ {
		j = (int(s[i]) + j + int(key[i%n])) % n
		s[i], s[j] = s[j], s[i]
	}

	// Pre-compute hash.
	// TypeScript: (this.hash * value) >>> 0  ← uint32 truncation after each multiply.
	// Go must use uint32 arithmetic so overflow behaviour is identical.
	hash := uint32(1)
	for i := 0; i < n; i++ {
		v := uint32(key[i])
		if v == 0 {
			continue
		}
		next := hash * v // uint32 — wraps at 2^32, same as JS >>> 0
		if next == 0 || next <= hash {
			break
		}
		hash = next
	}

	return &QmcRC4Cipher{key: key, n: n, s: s, hash: hash}
}

func (c *QmcRC4Cipher) getSegmentKey(id int) int {
	seed := int(c.key[id%c.n])
	idx := uint64(math.Floor(float64(c.hash) / float64((id+1)*seed) * 100.0))
	return int(idx % uint64(c.n))
}

// getFirstSegmentKey is the desktop decoder's special 1-based key selection
// for bytes [0, 0x80).  The divisor uses positions 1..128 while the seed is
// selected from the preceding zero-based key index.
func (c *QmcRC4Cipher) getFirstSegmentKey(position int) int {
	seed := int(c.key[(position-1)%c.n])
	idx := uint64(math.Floor(float64(c.hash) / float64(position*seed) * 100.0))
	return int(idx % uint64(c.n))
}

func (c *QmcRC4Cipher) encFirstSegment(buf []byte, offset int) {
	for i := range buf {
		buf[i] ^= c.key[c.getFirstSegmentKey(offset+i+1)]
	}
}

func (c *QmcRC4Cipher) encASegment(buf []byte, offset int) {
	// Clone the permutation box
	s := make([]byte, c.n)
	copy(s, c.s)

	segID := offset / rc4SegmentSize
	skipLen := (offset % rc4SegmentSize) + c.getSegmentKey(segID)

	j, k := 0, 0
	for i := -skipLen; i < len(buf); i++ {
		j = (j + 1) % c.n
		k = (int(s[j]) + k) % c.n
		s[j], s[k] = s[k], s[j]
		if i >= 0 {
			buf[i] ^= s[(int(s[j])+int(s[k]))%c.n]
		}
	}
}

// Decrypt decrypts buf in-place given its stream offset.
func (c *QmcRC4Cipher) Decrypt(buf []byte, offset int) {
	toProcess := len(buf)
	processed := 0

	// First segment (offset < 0x80)
	if offset < rc4FirstSegmentSize {
		segLen := toProcess
		if segLen > rc4FirstSegmentSize-offset {
			segLen = rc4FirstSegmentSize - offset
		}
		c.encFirstSegment(buf[:segLen], offset)
		toProcess -= segLen
		processed += segLen
		offset += segLen
		if toProcess == 0 {
			return
		}
	}

	// Align to segment boundary
	if offset%rc4SegmentSize != 0 {
		segLen := rc4SegmentSize - (offset % rc4SegmentSize)
		if segLen > toProcess {
			segLen = toProcess
		}
		c.encASegment(buf[processed:processed+segLen], offset)
		toProcess -= segLen
		processed += segLen
		offset += segLen
		if toProcess == 0 {
			return
		}
	}

	// Full segments
	for toProcess > rc4SegmentSize {
		c.encASegment(buf[processed:processed+rc4SegmentSize], offset)
		toProcess -= rc4SegmentSize
		processed += rc4SegmentSize
		offset += rc4SegmentSize
	}

	// Last partial segment
	if toProcess > 0 {
		c.encASegment(buf[processed:], offset)
	}
}
