//go:build windows

package decrypt

import "testing"

func TestQQMusicDeviceKeyFromParts(t *testing.T) {
	got, err := qqMusicDeviceKeyFromParts("001122334455", "ABCDEFGH", "MODEL", "FIRMWARE")
	if err != nil {
		t.Fatal(err)
	}
	const want = "8F88387ECDD7028BF2BA83F1A7712788"
	if got != want {
		t.Fatalf("device key = %s, want %s", got, want)
	}
}

func TestQQMusicSwapSerialPairs(t *testing.T) {
	if got, want := qqMusicSwapSerialPairs("ABCDE"), "BADCE"; got != want {
		t.Fatalf("swap = %q, want %q", got, want)
	}
}
