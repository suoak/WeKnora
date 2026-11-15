//go:build anydoc && cgo

package anydoc

import (
	"errors"
	"fmt"
	"testing"

	upstream "github.com/firecrawl/anydoc/go"
	"github.com/stretchr/testify/require"
)

func TestNativeResourceLimitRetainsTypedCause(t *testing.T) {
	cause := &upstream.ConvertError{Kind: "resource_limit", Detail: "synthetic bounded conversion refusal"}
	for _, err := range []error{cause, fmt.Errorf("converter: %w", cause)} {
		classified := classifyConversionError(err)
		require.ErrorIs(t, classified, ErrResourceLimit)
		var typed *upstream.ConvertError
		require.ErrorAs(t, classified, &typed)
		require.Same(t, cause, typed)
	}
}

func TestNativeResourceLimitCannotBeInferredFromText(t *testing.T) {
	var absent *upstream.ConvertError
	for _, err := range []error{
		nil, absent, errors.New("anydoc: resource_limit: bytes limit exceeded"),
		&upstream.ConvertError{Kind: "io", Detail: "resource_limit"},
		&upstream.ConvertError{Kind: "malformed", Detail: "resource_limit"},
		&upstream.ConvertError{Kind: "unsupported", Detail: "resource_limit"},
	} {
		classified := classifyConversionError(err)
		require.False(t, errors.Is(classified, ErrResourceLimit))
		require.Equal(t, err, classified)
	}
}
