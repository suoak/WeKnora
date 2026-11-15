package types

import "encoding/json"

// KnowledgeParseFailureKey reserves the server-owned recovery metadata entry.
const KnowledgeParseFailureKey = "parse_failure_v1"

// KnowledgeParseFailure describes server-owned recovery progress without raw
// parser diagnostics, file paths, source URLs, or credentials.
type KnowledgeParseFailure struct {
	Code        string `json:"code"`
	State       string `json:"state"`
	Action      string `json:"action"`
	Attempts    int    `json:"attempts"`
	MaxAttempts int    `json:"max_attempts"`
	AutoRetry   bool   `json:"auto_retry"`
}

// SetParseFailure updates recovery progress while preserving other metadata.
// A nil failure removes the previous processing attempt's recovery status.
func (k *Knowledge) SetParseFailure(failure *KnowledgeParseFailure) error {
	fields, err := k.Metadata.Map()
	if err != nil {
		return err
	}
	if fields == nil {
		fields = map[string]interface{}{}
	}
	if failure == nil {
		delete(fields, KnowledgeParseFailureKey)
	} else {
		fields[KnowledgeParseFailureKey] = failure
	}
	data, err := json.Marshal(fields)
	if err == nil {
		k.Metadata = JSON(data)
	}
	return err
}
