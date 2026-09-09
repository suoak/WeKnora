package types

// ModelPolicyRole identifies one independently configurable slot in the
// system-wide default model policy.
type ModelPolicyRole string

const (
	ModelPolicyRoleChat      ModelPolicyRole = "chat"
	ModelPolicyRoleSummary   ModelPolicyRole = "summary"
	ModelPolicyRoleEmbedding ModelPolicyRole = "embedding"
	ModelPolicyRoleRerank    ModelPolicyRole = "rerank"
	ModelPolicyRoleVLM       ModelPolicyRole = "vlm"
	ModelPolicyRoleASR       ModelPolicyRole = "asr"
)

// DefaultModelPolicy is deployment-wide and is not copied into tenants.
// Empty IDs explicitly mean that the corresponding system default is unset.
type DefaultModelPolicy struct {
	ChatModelID      string `json:"chat_model_id"`
	SummaryModelID   string `json:"summary_model_id"`
	EmbeddingModelID string `json:"embedding_model_id"`
	RerankModelID    string `json:"rerank_model_id"`
	VLMModelID       string `json:"vlm_model_id"`
	ASRModelID       string `json:"asr_model_id"`
}
