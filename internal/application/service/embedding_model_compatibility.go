package service

import (
	"fmt"
	"maps"
	"reflect"

	"github.com/Tencent/WeKnora/internal/types"
)

// A dimension match alone does not make two embedding spaces compatible.
// Keep the identity behind an existing ID stable; a different embedding
// configuration needs a new model ID and a separately approved reindex.
func validateEmbeddingIdentity(previous, next *types.Model) error {
	if previous == nil || previous.Type != types.ModelTypeEmbedding {
		return nil
	}
	before, after := previous.Parameters, next.Parameters
	if next.Type != previous.Type || next.Name != previous.Name || next.Source != previous.Source ||
		before.Provider != after.Provider || before.BaseURL != after.BaseURL ||
		before.InterfaceType != after.InterfaceType ||
		before.EmbeddingParameters != after.EmbeddingParameters || !maps.Equal(before.ExtraConfig, after.ExtraConfig) ||
		!maps.Equal(before.CustomHeaders, after.CustomHeaders) || !reflect.DeepEqual(before.Spec, after.Spec) {
		return fmt.Errorf("embedding model identity is immutable (dimension %d -> %d); "+
			"create a new model ID and obtain approval before rebuilding existing vector indexes",
			before.EmbeddingParameters.Dimension, after.EmbeddingParameters.Dimension)
	}
	return nil
}
