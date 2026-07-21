//go:build darwin

package decrypt

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf16"
)

const (
	qqMusicMacBundleID       = "com.tencent.QQMusicMac"
	qqMusicMacEKeyStoreIndex = 1
	qqMusicMacMMKVMetaSize   = 32
	qqMusicMacMMKVIVOffset   = 12
	qqMusicMacMMKVActualOff  = 28
)

// resolveQQMusicEKey reads the QQ Music for macOS EKey log. SetupHelper uses
// the complete downloaded-file path as the MMKV key, so the caller must retain
// the source path rather than the musicex footer's internal name.
func resolveQQMusicEKey(info QQMusicExInfo, options QQMusicOptions) (string, error) {
	if options.FilePath == "" {
		return "", errors.New("qqmusic/musicex: macOS resolver requires the absolute source FilePath")
	}
	if !filepath.IsAbs(options.FilePath) {
		return "", fmt.Errorf("qqmusic/musicex: macOS FilePath is not absolute: %q", options.FilePath)
	}

	appSupportPaths, err := qqMusicMacAppSupportPaths(options.MacAppSupportPath)
	if err != nil {
		return "", err
	}

	errs := make([]error, 0, len(appSupportPaths))
	for _, appSupportPath := range appSupportPaths {
		ekey, resolveErr := qqMusicMacResolveEKeyAt(appSupportPath, options.FilePath)
		if resolveErr == nil {
			return ekey, nil
		}
		errs = append(errs, fmt.Errorf("%s: %w", appSupportPath, resolveErr))
	}
	return "", fmt.Errorf("qqmusic/musicex: macOS EKey for %q (%q): %w", options.FilePath, info.InnerName, errors.Join(errs...))
}

func qqMusicMacResolveEKeyAt(appSupportPath, filePath string) (string, error) {
	errs := make([]error, 0, 2)
	for _, preferencesPath := range qqMusicMacPreferencesPaths(appSupportPath) {
		openUDID, err := qqMusicMacReadOpenUDID(preferencesPath)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		mmapID, encryptionKey, err := qqMusicMacGMCK(openUDID, qqMusicMacEKeyStoreIndex)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		plain, err := qqMusicMacReadMMKV(filepath.Join(appSupportPath, "iData", mmapID), encryptionKey)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		ekey, err := qqMusicMacEKeyFromMMKV(plain, filePath)
		if err == nil {
			return ekey, nil
		}
		errs = append(errs, err)
	}
	return "", errors.Join(errs...)
}

func qqMusicMacAppSupportPaths(override string) ([]string, error) {
	if override != "" {
		return []string{filepath.Clean(override)}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("qqmusic/musicex: find home directory: %w", err)
	}
	return []string{
		filepath.Join(
			home,
			"Library",
			"Containers",
			qqMusicMacBundleID,
			"Data",
			"Library",
			"Application Support",
			"QQMusicMac",
		),
		filepath.Join(home, "Library", "Application Support", "QQMusicMac"),
	}, nil
}

func qqMusicMacPreferencesPath(appSupportPath string) string {
	// appSupportPath is .../Data/Library/Application Support/QQMusicMac.
	libraryPath := filepath.Dir(filepath.Dir(appSupportPath))
	return filepath.Join(libraryPath, "Preferences", qqMusicMacBundleID+".plist")
}

// qqMusicMacPreferencesPaths tries the preference file paired with the data
// root first, then the sandbox and non-sandbox variants. Some client upgrades
// retain the OpenUDID in only one of the two Library trees.
func qqMusicMacPreferencesPaths(appSupportPath string) []string {
	paths := []string{qqMusicMacPreferencesPath(appSupportPath)}
	home, err := os.UserHomeDir()
	if err != nil {
		return paths
	}
	for _, path := range []string{
		filepath.Join(home, "Library", "Containers", qqMusicMacBundleID, "Data", "Library", "Preferences", qqMusicMacBundleID+".plist"),
		filepath.Join(home, "Library", "Preferences", qqMusicMacBundleID+".plist"),
	} {
		if path == paths[0] {
			continue
		}
		paths = append(paths, path)
	}
	return paths
}

func qqMusicMacReadOpenUDID(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("qqmusic/musicex: read macOS preferences %s: %w", path, err)
	}

	var openUDID string
	if bytes.HasPrefix(raw, []byte("bplist00")) {
		openUDID, err = qqMusicMacOpenUDIDFromBinaryPlist(raw)
	} else {
		openUDID, err = qqMusicMacOpenUDIDFromXMLPlist(raw)
	}
	if err != nil {
		return "", fmt.Errorf("qqmusic/musicex: read OpenUDID from %s: %w", path, err)
	}
	return strings.TrimSpace(openUDID), nil
}

// qqMusicMacGMCK mirrors ComHelper's _gmck(i, mode) for the two values used
// by SetupHelper. mode=1 derives a short MMKV ID; mode=0 derives its AES key.
func qqMusicMacGMCK(openUDID string, index int) (mmapID, encryptionKey string, err error) {
	if index < 0 {
		return "", "", fmt.Errorf("qqmusic/musicex: invalid macOS MMKV index %d", index)
	}
	if len(openUDID) < 7 {
		return "", "", errors.New("qqmusic/musicex: OpenUDID is too short")
	}

	lengthSeed, parseErr := strconv.ParseUint(openUDID[5:7], 16, 8)
	if parseErr != nil {
		return "", "", fmt.Errorf("qqmusic/musicex: OpenUDID length seed: %w", parseErr)
	}

	rotated := qqMusicMacRotateID(openUDID, index+3)
	idLength := 5 + int((lengthSeed+uint64(index))%4)
	if idLength > len(rotated) {
		return "", "", errors.New("qqmusic/musicex: OpenUDID cannot form MMKV ID")
	}

	suffix := fmt.Sprintf("%02x", 0xa546+index)
	digest := md5.Sum(append(append([]byte(nil), openUDID...), suffix...))
	return rotated[:idLength], hex.EncodeToString(digest[:]), nil
}

func qqMusicMacRotateID(value string, shift int) string {
	buf := []byte(value)
	for i, b := range buf {
		switch {
		case b >= 'A' && b <= 'Z':
			buf[i] = 'A' + (b-'A'+byte(shift%26))%26
		case b >= 'a' && b <= 'z':
			buf[i] = 'a' + (b-'a'+byte(shift%26))%26
		case b >= '0' && b <= '9':
			buf[i] = '0' + (b-'0'+byte(shift%10))%10
		}
	}
	return string(buf)
}

func qqMusicMacReadMMKV(dataPath, encryptionKey string) ([]byte, error) {
	if len(encryptionKey) < aes.BlockSize {
		return nil, errors.New("qqmusic/musicex: macOS MMKV encryption key is too short")
	}

	const attempts = 3
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		metaBefore, err := os.ReadFile(dataPath + ".crc")
		if err != nil {
			return nil, fmt.Errorf("qqmusic/musicex: read macOS MMKV metadata %s.crc: %w", dataPath, err)
		}
		data, err := os.ReadFile(dataPath)
		if err != nil {
			return nil, fmt.Errorf("qqmusic/musicex: read macOS MMKV data %s: %w", dataPath, err)
		}
		metaAfter, err := os.ReadFile(dataPath + ".crc")
		if err != nil {
			return nil, fmt.Errorf("qqmusic/musicex: reread macOS MMKV metadata %s.crc: %w", dataPath, err)
		}
		if !qqMusicMacSameMMKVMeta(metaBefore, metaAfter) {
			lastErr = errors.New("MMKV metadata changed while reading")
			continue
		}

		plain, err := qqMusicMacDecryptMMKV(data, metaAfter, encryptionKey)
		if err == nil {
			return plain, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("qqmusic/musicex: macOS MMKV snapshot did not stabilise: %w", lastErr)
}

func qqMusicMacSameMMKVMeta(first, second []byte) bool {
	return len(first) >= qqMusicMacMMKVMetaSize &&
		len(second) >= qqMusicMacMMKVMetaSize &&
		bytes.Equal(first[:qqMusicMacMMKVMetaSize], second[:qqMusicMacMMKVMetaSize])
}

func qqMusicMacDecryptMMKV(data, meta []byte, encryptionKey string) ([]byte, error) {
	if len(meta) < qqMusicMacMMKVMetaSize {
		return nil, errors.New("qqmusic/musicex: macOS MMKV metadata is too short")
	}
	if binary.LittleEndian.Uint32(meta[4:8]) < 2 {
		return nil, errors.New("qqmusic/musicex: macOS MMKV metadata has no encryption IV")
	}
	if len(data) < 4 {
		return nil, errors.New("qqmusic/musicex: macOS MMKV data is too short")
	}

	actualSize := binary.LittleEndian.Uint32(data[:4])
	if actualSize == 0 {
		return nil, errors.New("qqmusic/musicex: macOS MMKV data is empty")
	}
	if uint64(actualSize) > uint64(len(data)-4) {
		return nil, fmt.Errorf("qqmusic/musicex: invalid macOS MMKV actual size %d", actualSize)
	}
	if binary.LittleEndian.Uint32(meta[4:8]) >= 3 &&
		binary.LittleEndian.Uint32(meta[qqMusicMacMMKVActualOff:qqMusicMacMMKVActualOff+4]) != actualSize {
		return nil, errors.New("qqmusic/musicex: macOS MMKV data and metadata actual sizes differ")
	}
	if len(encryptionKey) < aes.BlockSize {
		return nil, errors.New("qqmusic/musicex: macOS MMKV encryption key is too short")
	}

	block, err := aes.NewCipher([]byte(encryptionKey[:aes.BlockSize]))
	if err != nil {
		return nil, fmt.Errorf("qqmusic/musicex: initialise macOS MMKV AES: %w", err)
	}
	ciphertext := data[4 : 4+int(actualSize)]
	plain := make([]byte, len(ciphertext))
	cipher.NewCFBDecrypter(block, meta[qqMusicMacMMKVIVOffset:qqMusicMacMMKVIVOffset+aes.BlockSize]).XORKeyStream(plain, ciphertext)
	return plain, nil
}

// qqMusicMacEKeyFromMMKV parses MMKV's MiniPB map and applies its last-write
// semantics for the complete downloaded-file path.
func qqMusicMacEKeyFromMMKV(plain []byte, filePath string) (string, error) {
	position := 0
	if _, err := qqMusicMacReadUVarint(plain, &position); err != nil {
		return "", fmt.Errorf("qqmusic/musicex: read macOS MMKV root: %w", err)
	}

	var value []byte
	found := false
	for position < len(plain) {
		key, err := qqMusicMacReadMiniPBData(plain, &position)
		if err != nil {
			return "", fmt.Errorf("qqmusic/musicex: read macOS MMKV key: %w", err)
		}
		recordValue, err := qqMusicMacReadMiniPBData(plain, &position)
		if err != nil {
			return "", fmt.Errorf("qqmusic/musicex: read macOS MMKV value: %w", err)
		}
		if string(key) == filePath {
			value = recordValue
			found = true
		}
	}
	if !found || len(value) == 0 {
		return "", fmt.Errorf("qqmusic/musicex: no macOS EKey record for %q", filePath)
	}

	valuePosition := 0
	ekeyData, err := qqMusicMacReadMiniPBData(value, &valuePosition)
	if err != nil {
		return "", fmt.Errorf("qqmusic/musicex: read macOS EKey value: %w", err)
	}
	if valuePosition != len(value) {
		return "", errors.New("qqmusic/musicex: macOS EKey value has trailing data")
	}
	if len(ekeyData) != 364 && len(ekeyData) != 704 {
		return "", fmt.Errorf("qqmusic/musicex: unexpected macOS EKey length %d", len(ekeyData))
	}
	if _, err := base64.StdEncoding.DecodeString(string(ekeyData)); err != nil {
		return "", fmt.Errorf("qqmusic/musicex: macOS EKey is not base64: %w", err)
	}
	return string(ekeyData), nil
}

func qqMusicMacReadMiniPBData(data []byte, position *int) ([]byte, error) {
	length, err := qqMusicMacReadUVarint(data, position)
	if err != nil {
		return nil, err
	}
	if length > uint64(len(data)-*position) {
		return nil, errors.New("truncated MiniPB data")
	}
	end := *position + int(length)
	result := data[*position:end]
	*position = end
	return result, nil
}

func qqMusicMacReadUVarint(data []byte, position *int) (uint64, error) {
	var value uint64
	for i := 0; i < 10; i++ {
		if *position >= len(data) {
			return 0, errors.New("truncated MiniPB varint")
		}
		b := data[*position]
		*position = *position + 1
		if i == 9 && b > 1 {
			return 0, errors.New("MiniPB varint overflows uint64")
		}
		value |= uint64(b&0x7f) << (7 * i)
		if b&0x80 == 0 {
			return value, nil
		}
	}
	return 0, errors.New("malformed MiniPB varint")
}

// Binary plist support is deliberately limited to the dict/string objects
// needed for OpenUDID. QQ Music's normal preferences file is bplist00.
type qqMusicMacBinaryPlist struct {
	raw             []byte
	offsetSize      int
	objectRefSize   int
	objectCount     uint64
	topObject       uint64
	offsetTableFrom int
}

func qqMusicMacOpenUDIDFromBinaryPlist(raw []byte) (string, error) {
	plist, err := newQQMusicMacBinaryPlist(raw)
	if err != nil {
		return "", err
	}
	openUDIDObject, found, err := plist.dictValue(plist.topObject, "OpenUDID")
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("OpenUDID dictionary is missing")
	}
	valueObject, found, err := plist.dictValue(openUDIDObject, "OpenUDID")
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("OpenUDID value is missing")
	}
	return plist.stringValue(valueObject)
}

func newQQMusicMacBinaryPlist(raw []byte) (*qqMusicMacBinaryPlist, error) {
	const headerSize = 8
	const trailerSize = 32
	if len(raw) < headerSize+trailerSize || !bytes.HasPrefix(raw, []byte("bplist00")) {
		return nil, errors.New("invalid binary plist header")
	}
	trailer := raw[len(raw)-trailerSize:]
	offsetSize := int(trailer[6])
	objectRefSize := int(trailer[7])
	if offsetSize < 1 || offsetSize > 8 || objectRefSize < 1 || objectRefSize > 8 {
		return nil, errors.New("invalid binary plist integer sizes")
	}
	objectCount, err := qqMusicMacBigEndianUint(trailer[8:16])
	if err != nil || objectCount == 0 {
		return nil, errors.New("invalid binary plist object count")
	}
	topObject, err := qqMusicMacBigEndianUint(trailer[16:24])
	if err != nil || topObject >= objectCount {
		return nil, errors.New("invalid binary plist top object")
	}
	offsetTable, err := qqMusicMacBigEndianUint(trailer[24:32])
	if err != nil || offsetTable > uint64(len(raw)-trailerSize) {
		return nil, errors.New("invalid binary plist offset table")
	}
	if objectCount > uint64((len(raw)-int(offsetTable))/offsetSize) {
		return nil, errors.New("truncated binary plist offset table")
	}

	return &qqMusicMacBinaryPlist{
		raw:             raw,
		offsetSize:      offsetSize,
		objectRefSize:   objectRefSize,
		objectCount:     objectCount,
		topObject:       topObject,
		offsetTableFrom: int(offsetTable),
	}, nil
}

func (plist *qqMusicMacBinaryPlist) dictValue(object uint64, wantedKey string) (uint64, bool, error) {
	offset, err := plist.objectOffset(object)
	if err != nil {
		return 0, false, err
	}
	if plist.raw[offset]>>4 != 0xd {
		return 0, false, errors.New("binary plist object is not a dictionary")
	}
	count, refsFrom, err := plist.objectLength(offset)
	if err != nil {
		return 0, false, err
	}
	if count > uint64((plist.offsetTableFrom-refsFrom)/(2*plist.objectRefSize)) {
		return 0, false, errors.New("truncated binary plist dictionary")
	}
	for index := uint64(0); index < count; index++ {
		keyRefFrom := refsFrom + int(index)*plist.objectRefSize
		keyObject, err := plist.objectReference(keyRefFrom)
		if err != nil {
			return 0, false, err
		}
		key, err := plist.stringValue(keyObject)
		if err != nil {
			return 0, false, err
		}
		if key != wantedKey {
			continue
		}
		valueRefFrom := refsFrom + int(count)*plist.objectRefSize + int(index)*plist.objectRefSize
		valueObject, err := plist.objectReference(valueRefFrom)
		if err != nil {
			return 0, false, err
		}
		return valueObject, true, nil
	}
	return 0, false, nil
}

func (plist *qqMusicMacBinaryPlist) stringValue(object uint64) (string, error) {
	offset, err := plist.objectOffset(object)
	if err != nil {
		return "", err
	}
	kind := plist.raw[offset] >> 4
	length, dataFrom, err := plist.objectLength(offset)
	if err != nil {
		return "", err
	}
	switch kind {
	case 0x5:
		if length > uint64(plist.offsetTableFrom-dataFrom) {
			return "", errors.New("truncated binary plist ASCII string")
		}
		return string(plist.raw[dataFrom : dataFrom+int(length)]), nil
	case 0x6:
		if length > uint64((plist.offsetTableFrom-dataFrom)/2) {
			return "", errors.New("truncated binary plist UTF-16 string")
		}
		units := make([]uint16, int(length))
		for i := range units {
			units[i] = binary.BigEndian.Uint16(plist.raw[dataFrom+i*2 : dataFrom+i*2+2])
		}
		return string(utf16.Decode(units)), nil
	default:
		return "", errors.New("binary plist object is not a string")
	}
}

func (plist *qqMusicMacBinaryPlist) objectOffset(object uint64) (int, error) {
	if object >= plist.objectCount {
		return 0, errors.New("binary plist object reference is out of range")
	}
	from := plist.offsetTableFrom + int(object)*plist.offsetSize
	offset, err := qqMusicMacBigEndianUint(plist.raw[from : from+plist.offsetSize])
	if err != nil || offset >= uint64(plist.offsetTableFrom) {
		return 0, errors.New("binary plist object offset is out of range")
	}
	return int(offset), nil
}

func (plist *qqMusicMacBinaryPlist) objectReference(from int) (uint64, error) {
	if from < 0 || from+plist.objectRefSize > plist.offsetTableFrom {
		return 0, errors.New("binary plist object reference is truncated")
	}
	reference, err := qqMusicMacBigEndianUint(plist.raw[from : from+plist.objectRefSize])
	if err != nil || reference >= plist.objectCount {
		return 0, errors.New("binary plist object reference is out of range")
	}
	return reference, nil
}

func (plist *qqMusicMacBinaryPlist) objectLength(offset int) (uint64, int, error) {
	if offset < 0 || offset >= plist.offsetTableFrom {
		return 0, 0, errors.New("binary plist object is out of range")
	}
	lengthNibble := plist.raw[offset] & 0x0f
	if lengthNibble < 0x0f {
		return uint64(lengthNibble), offset + 1, nil
	}
	extendedFrom := offset + 1
	if extendedFrom >= plist.offsetTableFrom || plist.raw[extendedFrom]>>4 != 0x1 {
		return 0, 0, errors.New("invalid binary plist extended length")
	}
	byteCount := 1 << (plist.raw[extendedFrom] & 0x0f)
	if byteCount < 1 || byteCount > 8 || extendedFrom+1+byteCount > plist.offsetTableFrom {
		return 0, 0, errors.New("invalid binary plist extended length size")
	}
	length, err := qqMusicMacBigEndianUint(plist.raw[extendedFrom+1 : extendedFrom+1+byteCount])
	if err != nil {
		return 0, 0, err
	}
	return length, extendedFrom + 1 + byteCount, nil
}

func qqMusicMacBigEndianUint(raw []byte) (uint64, error) {
	if len(raw) == 0 || len(raw) > 8 {
		return 0, errors.New("invalid binary plist integer")
	}
	var value uint64
	for _, b := range raw {
		value = value<<8 | uint64(b)
	}
	return value, nil
}

type qqMusicMacXMLNode struct {
	name     string
	text     strings.Builder
	children []*qqMusicMacXMLNode
}

func qqMusicMacOpenUDIDFromXMLPlist(raw []byte) (string, error) {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	for {
		token, err := decoder.Token()
		if err != nil {
			return "", fmt.Errorf("parse XML plist: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		root, err := qqMusicMacReadXMLNode(decoder, start, 0)
		if err != nil {
			return "", err
		}
		if root.name != "plist" {
			continue
		}
		for _, child := range root.children {
			if child.name != "dict" {
				continue
			}
			outer := qqMusicMacXMLDictValue(child, "OpenUDID")
			if outer == nil || outer.name != "dict" {
				return "", errors.New("OpenUDID dictionary is missing")
			}
			value := qqMusicMacXMLDictValue(outer, "OpenUDID")
			if value == nil || value.name != "string" {
				return "", errors.New("OpenUDID value is missing")
			}
			return value.text.String(), nil
		}
		return "", errors.New("XML plist root dictionary is missing")
	}
}

func qqMusicMacReadXMLNode(decoder *xml.Decoder, start xml.StartElement, depth int) (*qqMusicMacXMLNode, error) {
	if depth > 64 {
		return nil, errors.New("XML plist nesting is too deep")
	}
	node := &qqMusicMacXMLNode{name: start.Name.Local}
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		switch token := token.(type) {
		case xml.CharData:
			node.text.Write([]byte(token))
		case xml.StartElement:
			child, err := qqMusicMacReadXMLNode(decoder, token, depth+1)
			if err != nil {
				return nil, err
			}
			node.children = append(node.children, child)
		case xml.EndElement:
			if token.Name == start.Name {
				return node, nil
			}
			return nil, errors.New("malformed XML plist")
		}
	}
}

func qqMusicMacXMLDictValue(dict *qqMusicMacXMLNode, wantedKey string) *qqMusicMacXMLNode {
	for index := 0; index+1 < len(dict.children); index++ {
		key := dict.children[index]
		if key.name == "key" && strings.TrimSpace(key.text.String()) == wantedKey {
			return dict.children[index+1]
		}
	}
	return nil
}
