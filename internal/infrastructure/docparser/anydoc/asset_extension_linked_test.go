//go:build anydoc && cgo

package anydoc

import "testing"

func TestUnknownAssetExtensionMatchesNativeSerializer(t *testing.T) {
	for _, media := range []string{"image/x-emf", "image/emf", "image/x-wmf", "application/vnd.ms-ole-object"} {
		if got := extensionFor(media); got != ".bin" {
			t.Fatalf("media %q detached from native .bin placement: %q", media, got)
		}
	}
}
