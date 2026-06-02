package decrypt

import (
	"bytes"
	"crypto/aes"
	"encoding/binary"
	"testing"
)

func TestSniffAudioExtDetectsFtypAtom(t *testing.T) {
	data := []byte{0x00, 0x00, 0x00, 0x18, 'f', 't', 'y', 'p', 'M', '4', 'A', ' '}
	if got := SniffAudioExt(data); got != "m4a" {
		t.Fatalf("SniffAudioExt() = %q, want m4a", got)
	}
}

func TestAesECBDecryptRejectsMalformedPKCS7Padding(t *testing.T) {
	key := []byte("0123456789abcdef")
	validCipher := aesECBEncryptForTest(t, []byte("hello"+string(bytes.Repeat([]byte{11}, 11))), key)
	plain, err := aesECBDecrypt(validCipher, key)
	if err != nil {
		t.Fatalf("valid decrypt failed: %v", err)
	}
	if string(plain) != "hello" {
		t.Fatalf("valid decrypt = %q, want hello", plain)
	}

	malformedCipher := aesECBEncryptForTest(t, []byte("hello"+string(bytes.Repeat([]byte{11}, 10))+string([]byte{10})), key)
	_, err = aesECBDecrypt(malformedCipher, key)
	if err == nil {
		t.Fatal("expected malformed padding error")
	}
}

func aesECBEncryptForTest(t *testing.T, plain, key []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain)%block.BlockSize() != 0 {
		t.Fatalf("test plaintext length %d is not a block multiple", len(plain))
	}
	out := make([]byte, len(plain))
	for i := 0; i < len(plain); i += block.BlockSize() {
		block.Encrypt(out[i:i+block.BlockSize()], plain[i:i+block.BlockSize()])
	}
	return out
}

func TestDecryptKgmRejectsHeaderInsideFixedHeader(t *testing.T) {
	data := make([]byte, 0x40)
	copy(data, kgmMagicHeader)
	binary.LittleEndian.PutUint32(data[0x10:0x14], 0x20)

	if _, err := DecryptKgm(data, false); err == nil {
		t.Fatal("expected invalid header length error")
	}
}

func TestDecryptXmRejectsDataOffsetPastPayload(t *testing.T) {
	data := make([]byte, 0x20)
	copy(data[0:4], xmMagic)
	copy(data[4:8], " MP3")
	copy(data[8:12], xmMagic2)
	data[0x0C], data[0x0D], data[0x0E] = 0x20, 0x00, 0x00

	if _, err := DecryptXm(data); err == nil {
		t.Fatal("expected data offset error")
	}
}

func TestDecryptTmRejectsShortFile(t *testing.T) {
	if _, err := DecryptTm([]byte{1, 2, 3}); err == nil {
		t.Fatal("expected short file error")
	}
}

func TestDecryptMg3dSearchesDocumentedKeyRange(t *testing.T) {
	key := []byte("0123456789ABCDEF0123456789ABCDEF")
	plain := make([]byte, 0x300)
	copy(plain[0:4], "RIFF")
	copy(plain[8:16], "WAVEfmt ")
	binary.LittleEndian.PutUint32(plain[16:20], 16)
	copy(plain[36:40], "data")
	binary.LittleEndian.PutUint32(plain[40:44], 0x1000)

	encrypted := make([]byte, len(plain))
	for i := range encrypted {
		encrypted[i] = plain[i] + key[i%len(key)]
	}
	copy(encrypted[0x280:0x280+len(key)], key)

	result, err := DecryptMg3d(encrypted)
	if err != nil {
		t.Fatalf("DecryptMg3d returned error: %v", err)
	}
	if !bytes.Equal(result.Audio[:0x40], plain[:0x40]) {
		t.Fatal("decrypted WAV header does not match expected plaintext")
	}
}
