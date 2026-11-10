package types

// FeishuLatestSnapshotAudit records a replacement of a historical content
// baseline at the same provider timestamp. The provider revision is unchanged;
// the new local attempt/generation identifies the freshly verified snapshot.
type FeishuLatestSnapshotAudit struct {
	PreviousAttempt    string `json:"previous_attempt"`
	PreviousGeneration int64  `json:"previous_generation"`
	PreviousHash       string `json:"previous_hash"`
	CurrentHash        string `json:"current_hash"`
}
