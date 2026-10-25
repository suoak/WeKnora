package repository

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/Tencent/WeKnora/internal/types"
)

// The legacy staging adapter carries metadata as strings. Preserve the typed
// processing configuration before hashing its manifest, then compare it again
// under the publication row lock so a concurrent user edit cannot be lost.
func legacyProcessMetadata(previous, candidate *types.Knowledge, normalize bool) error {
	if previous.ID == "" {
		return nil
	}
	if _, err := previous.ProcessOverrides(); err != nil {
		return legacyFence("knowledge_process_config_invalid")
	}
	old, err := previous.Metadata.Map()
	if err != nil {
		return err
	}
	next, err := candidate.Metadata.Map()
	if err != nil {
		return err
	}
	const key = "process_overrides"
	if reflect.DeepEqual(old[key], next[key]) {
		return nil
	}
	flattened, ok := next[key].(string)
	if !normalize || !ok || old[key] == nil || flattened != fmt.Sprintf("%v", old[key]) {
		return legacyFence("knowledge_process_config_changed")
	}
	next[key] = old[key]
	blob, err := json.Marshal(next)
	if err != nil {
		return err
	}
	candidate.Metadata = types.JSON(blob)
	return nil
}
