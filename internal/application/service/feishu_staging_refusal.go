package service

// feishuStagingRefusal identifies a document shape unsupported by safe staging.
// The old published version remains untouched and the document must be retried.
type feishuStagingRefusal struct{ reason string }

func (e *feishuStagingRefusal) Error() string { return "feishu safe staging refused: " + e.reason }
func (e *feishuStagingRefusal) Unwrap() error { return ErrFeishuStagingRequired }
