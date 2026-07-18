//go:build windows

package decrypt

import (
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unsafe"
)

const (
	qqMusicNetworkClassKey = `SYSTEM\CurrentControlSet\Control\Network\{4D36E972-E325-11CE-BFC1-08002BE10318}`

	qqMusicHKEYLocalMachine = 0x80000002
	qqMusicKeyRead          = 0x20019
	qqMusicRegSZ            = 1

	qqMusicErrorBufferOverflow = 111
	qqMusicFileShareRead       = 0x00000001
	qqMusicFileShareWrite      = 0x00000002
	qqMusicOpenExisting        = 3

	qqMusicIOCTLStorageQueryProperty = 0x002D1400
	qqMusicStorageDeviceProperty     = 0
	qqMusicPropertyStandardQuery     = 0
)

var (
	qqMusicIphlpapi     = syscall.NewLazyDLL("iphlpapi.dll")
	qqMusicGetAdapters  = qqMusicIphlpapi.NewProc("GetAdaptersInfo")
	qqMusicAdvapi32     = syscall.NewLazyDLL("advapi32.dll")
	qqMusicRegOpenKey   = qqMusicAdvapi32.NewProc("RegOpenKeyExW")
	qqMusicRegQuery     = qqMusicAdvapi32.NewProc("RegQueryValueExW")
	qqMusicRegClose     = qqMusicAdvapi32.NewProc("RegCloseKey")
	qqMusicKernel32     = syscall.NewLazyDLL("kernel32.dll")
	qqMusicCreateFile   = qqMusicKernel32.NewProc("CreateFileW")
	qqMusicCloseHandle  = qqMusicKernel32.NewProc("CloseHandle")
	qqMusicDeviceIOCtrl = qqMusicKernel32.NewProc("DeviceIoControl")

	qqMusicDeviceKeySalt = [8]byte{0x5C, 0xBD, 0x98, 0x7C, 0x1C, 0x38, 0x17, 0x8E}
)

// ipAdapterInfo contains the portion of IP_ADAPTER_INFO used by the device
// key algorithm. The native Next pointer allows traversal of the buffer
// returned by GetAdaptersInfo on both x86 and x64 builds.
type ipAdapterInfo struct {
	Next          *ipAdapterInfo
	ComboIndex    uint32
	AdapterName   [260]byte
	Description   [132]byte
	AddressLength uint32
	Address       [8]byte
	Index         uint32
	Type          uint32
}

type qqMusicDiskIdentity struct {
	Serial   string
	Model    string
	Firmware string
}

// qqMusicDeviceMMKVKey reproduces the device-key generator used to encrypt
// QQ Music's Checkccae.dat. It does not load QQ Music DLLs.
func qqMusicDeviceMMKVKey() (string, error) {
	mac, err := qqMusicPCIMAC()
	if err != nil {
		return "", err
	}
	disk, err := qqMusicPrimaryDiskIdentity()
	if err != nil {
		return "", err
	}
	return qqMusicDeviceKeyFromParts(hex.EncodeToString(mac), disk.Serial, disk.Model, disk.Firmware)
}

func qqMusicPCIMAC() ([]byte, error) {
	var size uint32
	status, _, _ := qqMusicGetAdapters.Call(0, uintptr(unsafe.Pointer(&size)))
	if status != qqMusicErrorBufferOverflow || size == 0 {
		return nil, fmt.Errorf("qqmusic/mmkv: GetAdaptersInfo size query: %w", syscall.Errno(status))
	}

	buffer := make([]byte, size)
	status, _, _ = qqMusicGetAdapters.Call(
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if status != 0 {
		return nil, fmt.Errorf("qqmusic/mmkv: GetAdaptersInfo: %w", syscall.Errno(status))
	}

	for ptr := unsafe.Pointer(&buffer[0]); ptr != nil; {
		adapter := (*ipAdapterInfo)(ptr)
		if adapter.AddressLength == 6 && qqMusicAdapterIsPCI(cString(adapter.AdapterName[:])) {
			mac := make([]byte, 6)
			copy(mac, adapter.Address[:6])
			return mac, nil
		}
		if adapter.Next == nil {
			break
		}
		ptr = unsafe.Pointer(adapter.Next)
	}
	return nil, errors.New("qqmusic/mmkv: no PCI network adapter with a MAC address")
}

func qqMusicAdapterIsPCI(adapterName string) bool {
	if adapterName == "" {
		return false
	}
	value, err := qqMusicRegistryString(
		qqMusicNetworkClassKey+`\`+adapterName+`\Connection`,
		"PnpInstanceID",
	)
	return err == nil && strings.HasPrefix(strings.ToUpper(value), "PCI\\")
}

func qqMusicRegistryString(path, valueName string) (string, error) {
	pathPtr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	valuePtr, err := syscall.UTF16PtrFromString(valueName)
	if err != nil {
		return "", err
	}

	var key uintptr
	status, _, _ := qqMusicRegOpenKey.Call(
		qqMusicHKEYLocalMachine,
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		qqMusicKeyRead,
		uintptr(unsafe.Pointer(&key)),
	)
	if status != 0 {
		return "", syscall.Errno(status)
	}
	defer qqMusicRegClose.Call(key)

	var valueType, byteCount uint32
	status, _, _ = qqMusicRegQuery.Call(
		key,
		uintptr(unsafe.Pointer(valuePtr)),
		0,
		uintptr(unsafe.Pointer(&valueType)),
		0,
		uintptr(unsafe.Pointer(&byteCount)),
	)
	if status != 0 {
		return "", syscall.Errno(status)
	}
	if valueType != qqMusicRegSZ || byteCount < 2 {
		return "", errors.New("unexpected registry value type")
	}

	data := make([]uint16, (byteCount+1)/2)
	status, _, _ = qqMusicRegQuery.Call(
		key,
		uintptr(unsafe.Pointer(valuePtr)),
		0,
		uintptr(unsafe.Pointer(&valueType)),
		uintptr(unsafe.Pointer(&data[0])),
		uintptr(unsafe.Pointer(&byteCount)),
	)
	if status != 0 {
		return "", syscall.Errno(status)
	}
	return syscall.UTF16ToString(data), nil
}

func qqMusicPrimaryDiskIdentity() (qqMusicDiskIdentity, error) {
	var firstErr error
	for index := 0; index < 16; index++ {
		identity, err := qqMusicDiskIdentityAt(index)
		if err == nil {
			return identity, nil
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return qqMusicDiskIdentity{}, fmt.Errorf("qqmusic/mmkv: read physical disk identity: %w", firstErr)
}

func qqMusicDiskIdentityAt(index int) (qqMusicDiskIdentity, error) {
	pathPtr, err := syscall.UTF16PtrFromString(fmt.Sprintf(`\\.\PhysicalDrive%d`, index))
	if err != nil {
		return qqMusicDiskIdentity{}, err
	}
	handle, _, callErr := qqMusicCreateFile.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		qqMusicFileShareRead|qqMusicFileShareWrite,
		0,
		qqMusicOpenExisting,
		0,
		0,
	)
	if handle == ^uintptr(0) {
		return qqMusicDiskIdentity{}, fmt.Errorf("open PhysicalDrive%d: %w", index, callErr)
	}
	defer qqMusicCloseHandle.Call(handle)

	// STORAGE_PROPERTY_QUERY contains an AdditionalParameters[1] member and
	// therefore occupies 12 bytes after C structure alignment.
	query := [12]byte{qqMusicStorageDeviceProperty, 0, 0, 0, qqMusicPropertyStandardQuery}
	buffer := make([]byte, 4096)
	var bytesReturned uint32
	ok, _, callErr := qqMusicDeviceIOCtrl.Call(
		handle,
		qqMusicIOCTLStorageQueryProperty,
		uintptr(unsafe.Pointer(&query[0])),
		uintptr(len(query)),
		uintptr(unsafe.Pointer(&buffer[0])),
		uintptr(len(buffer)),
		uintptr(unsafe.Pointer(&bytesReturned)),
		0,
	)
	if ok == 0 {
		return qqMusicDiskIdentity{}, fmt.Errorf("query PhysicalDrive%d: %w", index, callErr)
	}
	if bytesReturned < 36 {
		return qqMusicDiskIdentity{}, errors.New("storage descriptor is too short")
	}

	model := strings.TrimSpace(storageDescriptorString(buffer, binary.LittleEndian.Uint32(buffer[16:20])))
	firmware := strings.TrimSpace(storageDescriptorString(buffer, binary.LittleEndian.Uint32(buffer[20:24])))
	serial := strings.TrimSpace(storageDescriptorString(buffer, binary.LittleEndian.Uint32(buffer[24:28])))
	if serial == "" || model == "" || firmware == "" {
		return qqMusicDiskIdentity{}, errors.New("storage descriptor is missing serial, model, or firmware")
	}
	return qqMusicDiskIdentity{Serial: serial, Model: model, Firmware: firmware}, nil
}

func storageDescriptorString(buffer []byte, offset uint32) string {
	if offset == 0 || int(offset) >= len(buffer) {
		return ""
	}
	return cString(buffer[offset:])
}

func qqMusicDeviceKeyFromParts(mac, serial, model, firmware string) (string, error) {
	if len(mac) != 12 {
		return "", fmt.Errorf("qqmusic/mmkv: invalid MAC length %d", len(mac))
	}
	serial = qqMusicSwapSerialPairs(serial)
	if serial == "" || model == "" || firmware == "" {
		return "", errors.New("qqmusic/mmkv: incomplete disk identity")
	}

	input := make([]byte, 0, len(mac)+len(serial)+len(model)+len(firmware)+len(qqMusicDeviceKeySalt))
	input = append(input, strings.ToLower(mac)...)
	input = append(input, serial...)
	input = append(input, model...)
	input = append(input, firmware...)
	input = append(input, qqMusicDeviceKeySalt[:]...)
	digest := md5.Sum(input)

	return fmt.Sprintf(
		"%08X%04X%04X%02X%02X%02X%02X%02X%02X%02X%02X",
		binary.LittleEndian.Uint32(digest[0:4]),
		binary.LittleEndian.Uint16(digest[4:6]),
		binary.LittleEndian.Uint16(digest[6:8]),
		digest[8], digest[9], digest[10], digest[11],
		digest[12], digest[13], digest[14], digest[15],
	), nil
}

func qqMusicSwapSerialPairs(serial string) string {
	bytes := []byte(serial)
	for i := 0; i+1 < len(bytes); i += 2 {
		bytes[i], bytes[i+1] = bytes[i+1], bytes[i]
	}
	return string(bytes)
}

func cString(data []byte) string {
	for i, b := range data {
		if b == 0 {
			return string(data[:i])
		}
	}
	return string(data)
}
