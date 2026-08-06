package upgrade

import (
	"runtime"
	"strings"
	"testing"
)

func TestExpectedAssetName(t *testing.T) {
	got := expectedAssetName()
	want := "jvm-" + runtime.GOOS + "-" + runtime.GOARCH + ".zip"
	if got != want {
		t.Errorf("expectedAssetName() = %q, want %q", got, want)
	}
	if !strings.HasSuffix(got, ".zip") {
		t.Errorf("expectedAssetName() 应以 .zip 结尾: %q", got)
	}
}
