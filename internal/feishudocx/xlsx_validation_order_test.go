package feishudocx

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func validationOrderWorkbook(t *testing.T, rules string) []byte {
	t.Helper()
	parts := embeddedWorkbookParts()
	parts["xl/worksheets/sheet1.xml"] = `<worksheet xmlns="` + spreadsheetNS +
		`"><sheetData/><dataValidations count="2">` + rules + `</dataValidations></worksheet>`
	return archive(t, parts, false)
}

func TestOrderedRevisionValidationRulesPreserveAuthoredContent(t *testing.T) {
	first := `<dataValidation type="list" sqref="A1:A3"><formula1>"one,two"</formula1></dataValidation>`
	second := `<dataValidation type="list" sqref="B1:B3"><formula1>"red,blue"</formula1></dataValidation>`
	a, b := validationOrderWorkbook(t, first+second), validationOrderWorkbook(t, second+first)
	left, algorithm, err := CanonicalOrderedRevisionXLSXSnapshotDigest(a)
	require.NoError(t, err)
	right, _, err := CanonicalOrderedRevisionXLSXSnapshotDigest(b)
	require.NoError(t, err)
	require.Equal(t, "feishu-xlsx-revision-bound-snapshot-v4", algorithm)
	require.Equal(t, left, right)
	oldLeft, _, err := CanonicalRevisionBoundXLSXSnapshotDigest(a)
	require.NoError(t, err)
	oldRight, _, err := CanonicalRevisionBoundXLSXSnapshotDigest(b)
	require.NoError(t, err)
	require.NotEqual(t, oldLeft, oldRight, "the existing v3 contract remains unchanged")
	for _, changed := range []string{
		strings.Replace(first+second, "one,two", "two,one", 1),
		strings.Replace(first+second, `type="list"`, `type="whole"`, 1),
		strings.Replace(first+second, `sqref="B1:B3"`, `sqref="C1:C3"`, 1),
	} {
		digest, _, err := CanonicalOrderedRevisionXLSXSnapshotDigest(validationOrderWorkbook(t, changed))
		require.NoError(t, err)
		require.NotEqual(t, left, digest, "formula options, rule types and ranges remain authoritative")
	}
	for _, ref := range []string{"A2:A5", "A3:B3", "XFE1", "B1048577", "B3:B1", "NamedRange"} {
		unsafe := strings.Replace(second, "B1:B3", ref, 1)
		_, _, err := CanonicalOrderedRevisionXLSXSnapshotDigest(validationOrderWorkbook(t, first+unsafe))
		require.ErrorIs(t, err, ErrInvalidXLSXPackage, ref)
	}
}
