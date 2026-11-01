package docparser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPackageImageReferencePreservesOtherContent(t *testing.T) {
	source := "before ![sheet](images/object.bin) after ![photo](images/photo.png)"
	result, count := ReplacePackageImageReference(source, "images/object.bin")
	require.Equal(t, 1, count)
	require.Equal(t, "before [Embedded workbook: complete worksheet content follows below]"+
		" after ![photo](images/photo.png)", result)
	result, count = ReplacePackageImageReference(source, "images/not-the-object.bin")
	require.Equal(t, 0, count)
	require.Equal(t, source, result)
}
