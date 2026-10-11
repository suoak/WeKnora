package format

import (
	"bytes"
	"testing"
)

func TestJQIntegerPrecisionAndInvalidTrailingData(t *testing.T) {
	for _, number := range []string{"9007199254740993", "18446744073709551615"} {
		var output bytes.Buffer
		if err := writeJQ(&output, []byte(`{"id":`+number+`}`), ".id"); err != nil {
			t.Fatal("valid integer projection failed")
		}
		if output.String() != number+"\n" {
			t.Fatal("integer projection lost precision")
		}
	}
	for _, input := range []string{`{"id":1}{"id":2}`, `{"id":1} trailing`} {
		var output bytes.Buffer
		if err := writeJQ(&output, []byte(input), ".id"); err == nil || output.Len() != 0 {
			t.Fatal("invalid trailing JSON was accepted")
		}
	}
}
