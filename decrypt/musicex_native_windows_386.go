//go:build windows && 386

package decrypt

// This file calls the current QQ Music desktop QMC decoder instead of the
// historical Map/RC4 implementation.  The musicex stream format changed from
// older QMC downloads; CommonFunction.dll owns the matching current decoder.

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

const (
	memCommit            = 0x1000
	memReserve           = 0x2000
	memRelease           = 0x8000
	pageExecuteReadWrite = 0x40
)

func qqMusicDecryptPayload(installDir string, payload []byte, rawExt, ekey string) (*QmcResult, error) {
	if len(payload) == 0 {
		return nil, fmt.Errorf("qqmusic/musicex: empty encrypted payload")
	}
	dll, err := loadQQMusicCommon(installDir)
	if err != nil {
		return nil, err
	}
	defer dll.Release()

	createStream, err := qqMusicProcByOrdinal(dll, 3)
	if err != nil {
		return nil, err
	}
	destroyStream, err := qqMusicProcByOrdinal(dll, 4)
	if err != nil {
		return nil, err
	}
	createKey, err := qqMusicProcByOrdinal(dll, 8)
	if err != nil {
		return nil, err
	}
	destroyKey, err := qqMusicProcByOrdinal(dll, 9)
	if err != nil {
		return nil, err
	}

	stream, _, callErr := syscall.Syscall(createStream, 3, 0, 0, 0)
	if stream == 0 || callFailed(callErr) {
		return nil, fmt.Errorf("qqmusic/musicex: create stream decoder: %w", callErr)
	}
	defer syscall.Syscall(destroyStream, 1, stream, 0, 0)

	var nativeErr uint32
	keyDecoder, _, callErr := syscall.Syscall(createKey, 1, uintptr(unsafe.Pointer(&nativeErr)), 0, 0)
	if keyDecoder == 0 || callFailed(callErr) {
		return nil, fmt.Errorf("qqmusic/musicex: create key decoder (code=%d): %w", nativeErr, callErr)
	}
	defer syscall.Syscall(destroyKey, 1, keyDecoder, 0, 0)

	setKey, err := qqMusicThiscallThunk(qqMusicVFunc(keyDecoder, 0), 8)
	if err != nil {
		return nil, err
	}
	defer qqMusicFreeThunk(setKey)
	setKeyDecoder, err := qqMusicThiscallThunk(qqMusicVFunc(stream, 8), 4)
	if err != nil {
		return nil, err
	}
	defer qqMusicFreeThunk(setKeyDecoder)
	crypt, err := qqMusicThiscallThunk(qqMusicVFunc(stream, 16), 16)
	if err != nil {
		return nil, err
	}
	defer qqMusicFreeThunk(crypt)

	keyBytes := []byte(ekey)
	if _, _, callErr := syscall.Syscall(setKey, 3, keyDecoder, uintptr(unsafe.Pointer(&keyBytes[0])), uintptr(len(keyBytes))); callFailed(callErr) {
		return nil, fmt.Errorf("qqmusic/musicex: set ekey: %w", callErr)
	}
	if _, _, callErr := syscall.Syscall(setKeyDecoder, 2, stream, keyDecoder, 0); callFailed(callErr) {
		return nil, fmt.Errorf("qqmusic/musicex: attach key decoder: %w", callErr)
	}

	audio := append([]byte(nil), payload...)
	lo := uintptr(uint32(0))
	hi := uintptr(uint32(0))
	if _, _, callErr := syscall.Syscall6(crypt, 5,
		stream, lo, hi, uintptr(unsafe.Pointer(&audio[0])), uintptr(len(audio)), 0); callFailed(callErr) {
		return nil, fmt.Errorf("qqmusic/musicex: decrypt payload: %w", callErr)
	}
	runtime.KeepAlive(keyBytes)
	runtime.KeepAlive(audio)
	if !HasKnownAudioMagic(audio) {
		return nil, fmt.Errorf("qqmusic/musicex: decrypted data has no recognised audio header; key or format version changed")
	}

	ext := SniffAudioExt(audio)
	if ext == "mp3" {
		ext = qmcExtHint(rawExt)
	}
	return &QmcResult{Audio: audio, Ext: ext, Mime: AudioMimeType(ext)}, nil
}

func qqMusicVFunc(object, offset uintptr) uintptr {
	vtable := *(*uintptr)(unsafe.Pointer(object))
	return *(*uintptr)(unsafe.Pointer(vtable + offset))
}

// qqMusicThiscallThunk converts a 32-bit MSVC thiscall virtual method into a
// stdcall function pointer.  Go's syscall.Syscall* can then invoke it.  The
// generated code is equivalent to the thunk used in qqmusic_decode.ps1.
func qqMusicThiscallThunk(target uintptr, argBytes int) (uintptr, error) {
	if target == 0 || argBytes < 0 || argBytes%4 != 0 {
		return 0, fmt.Errorf("qqmusic/native: invalid virtual method")
	}
	n := argBytes / 4
	code := make([]byte, 0, 4+n*4+5+2+3)
	code = append(code, 0x8B, 0x4C, 0x24, 0x04) // mov ecx,[esp+4] (this)
	offset := byte(4 + argBytes)
	for i := 0; i < n; i++ {
		code = append(code, 0xFF, 0x74, 0x24, offset) // push argument, right-to-left
	}
	code = append(code, 0xB8, byte(target), byte(target>>8), byte(target>>16), byte(target>>24))
	code = append(code, 0xFF, 0xD0) // call eax
	stackBytes := uint16(4 + argBytes)
	code = append(code, 0xC2, byte(stackBytes), byte(stackBytes>>8)) // ret this + args

	virtualAlloc := syscall.NewLazyDLL("kernel32.dll").NewProc("VirtualAlloc")
	mem, _, callErr := virtualAlloc.Call(0, uintptr(len(code)), memCommit|memReserve, pageExecuteReadWrite)
	if mem == 0 {
		return 0, fmt.Errorf("qqmusic/native: allocate call thunk: %w", callErr)
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(mem)), len(code)), code)
	return mem, nil
}

func qqMusicFreeThunk(mem uintptr) {
	if mem == 0 {
		return
	}
	syscall.NewLazyDLL("kernel32.dll").NewProc("VirtualFree").Call(mem, 0, memRelease)
}
