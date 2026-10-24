package types

import "errors"

// ErrFeishuStagingCapacity denies a candidate write without changing public data.
var ErrFeishuStagingCapacity = errors.New("Feishu staging disk capacity unavailable or insufficient")
