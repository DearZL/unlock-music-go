package decrypt

import "testing"

func TestNcmCoreKey(t *testing.T) {
	if got, want := string(ncmCoreKey), "hzHRAmso5kInbaxW"; got != want {
		t.Fatalf("ncmCoreKey = %q, want %q", got, want)
	}
}
