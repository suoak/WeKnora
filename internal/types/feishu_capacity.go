package types

import "errors"

// ErrFeishuStagingCapacity denies a candidate write without changing public data.
var ErrFeishuStagingCapacity = errors.New("feishu staging disk capacity unavailable or insufficient")
