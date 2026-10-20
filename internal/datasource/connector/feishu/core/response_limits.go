package core

import (
	"errors"
	"fmt"
	"io"
)

const (
	maxFeishuJSONBytes  int64 = 16 << 20
	maxFeishuErrorBytes int64 = 4 << 10
)

// ErrResponseTooLarge is deterministic and must never be retried or indexed
// as a truncated success. No provider response content is retained.
var ErrResponseTooLarge = errors.New("feishu response exceeds maximum size")

func readFeishuCapped(body io.Reader, limit int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("%w: limit=%d", ErrResponseTooLarge, limit)
	}
	return data, nil
}
