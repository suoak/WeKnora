package repository

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPersonalExactReviewedBaselineFailsClosed(t *testing.T) {
	t.Setenv("FEISHU_PERSONAL_EXACT_REVIEWED_RESUME_KNOWLEDGE_IDS", "")
	for _, raw := range []string{
		``, `{`, `null`, `[]`, `{"doc":"162"}`, `{"doc":161}`, `{"doc":163}`,
		`{"other":162}`, `{"doc":-1}`, `{"doc":10001}`, `{"doc":162} trailing`,
	} {
		t.Run(raw, func(t *testing.T) {
			t.Setenv("FEISHU_PERSONAL_EXACT_REVIEWED_RESUME_BASELINES", raw)
			require.Equal(t, PersonalExactResumeMaxAttempts, PersonalExactResumeBudget("doc", 162))
		})
	}
	t.Setenv("FEISHU_PERSONAL_EXACT_REVIEWED_RESUME_BASELINES", `{"doc":162}`)
	require.Equal(t, 163, PersonalExactResumeBudget("doc", 162))
	require.Equal(t, PersonalExactResumeMaxAttempts, PersonalExactResumeBudget("doc", 163))
	require.Equal(t, PersonalExactResumeMaxAttempts, PersonalExactResumeBudget("other", 162))
}
