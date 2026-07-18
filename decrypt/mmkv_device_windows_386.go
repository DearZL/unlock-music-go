//go:build windows && 386

package decrypt

// CommonFunction.dll is a 32-bit QQ Music component.  Ordinal 12 returns the
// same device-derived MMKV key that the desktop client uses for Checkccae.dat.

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

func qqMusicDeviceMMKVKey(installDir string) (string, error) {
	dll, err := loadQQMusicCommon(installDir)
	if err != nil {
		return "", err
	}
	defer dll.Release()

	proc, callErr := qqMusicProcByOrdinal(dll, 12)
	if proc == 0 {
		return "", fmt.Errorf("qqmusic/mmkv: find CommonFunction ordinal 12: %w", callErr)
	}

	first := make([]byte, 256)
	second := make([]byte, 256)
	// Ordinal 12 is stdcall: void GenDeviceKey(void *first, void *second).
	// The QQ Music API writes a NUL-terminated ASCII key to the first buffer.
	_, _, callErr = syscall.Syscall(proc, 2,
		uintptr(unsafe.Pointer(&first[0])), uintptr(unsafe.Pointer(&second[0])), 0)
	runtime.KeepAlive(first)
	runtime.KeepAlive(second)
	if callFailed(callErr) {
		return "", fmt.Errorf("qqmusic/mmkv: call CommonFunction ordinal 12: %w", callErr)
	}

	end := 0
	for end < len(first) && first[end] != 0 {
		end++
	}
	key := string(first[:end])
	if len(key) < 16 {
		return "", fmt.Errorf("qqmusic/mmkv: CommonFunction returned an invalid key %q", key)
	}
	return key, nil
}

func loadQQMusicCommon(installDir string) (*syscall.DLL, error) {
	setDLLDirectory := syscall.NewLazyDLL("kernel32.dll").NewProc("SetDllDirectoryW")
	dirPtr, err := syscall.UTF16PtrFromString(installDir)
	if err != nil {
		return nil, fmt.Errorf("qqmusic/native: invalid install directory: %w", err)
	}
	if ok, _, callErr := setDLLDirectory.Call(uintptr(unsafe.Pointer(dirPtr))); ok == 0 {
		return nil, fmt.Errorf("qqmusic/native: set DLL directory: %w", callErr)
	}
	defer setDLLDirectory.Call(0)

	dll, err := syscall.LoadDLL(installDir + `\CommonFunction.dll`)
	if err != nil {
		return nil, fmt.Errorf("qqmusic/native: load CommonFunction.dll: %w", err)
	}
	return dll, nil
}

func qqMusicProcByOrdinal(dll *syscall.DLL, ordinal uintptr) (uintptr, error) {
	getProcAddress := syscall.NewLazyDLL("kernel32.dll").NewProc("GetProcAddress")
	proc, _, callErr := getProcAddress.Call(uintptr(dll.Handle), ordinal)
	if proc == 0 {
		return 0, fmt.Errorf("qqmusic/native: find CommonFunction ordinal %d: %w", ordinal, callErr)
	}
	return proc, nil
}

// LazyProc.Call and Syscall return syscall.Errno(0) as a non-nil error
// interface on some Go/Windows combinations. Treat it as success.
func callFailed(err error) bool {
	if err == nil {
		return false
	}
	if errno, ok := err.(syscall.Errno); ok {
		return errno != 0
	}
	return true
}
