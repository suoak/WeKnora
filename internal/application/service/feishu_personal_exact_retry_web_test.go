package service

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestPersonalExactRetryWebReauthorizationIsBoundToOriginalOwner(t *testing.T) {
	for _, identity := range []string{"A", "B", "unrelated", "viewer"} {
		t.Run(identity, func(t *testing.T) {
			f, owner, sealed := sealedExactRetryFixture(t)
			user, role := identity, types.TenantRoleAdmin
			if identity == "viewer" {
				user, role = "B", types.TenantRoleViewer
				require.NoError(t, f.db.Exec("UPDATE tenant_members SET role='viewer' WHERE user_id='B'").Error)
			}
			t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY", "1")
			t.Setenv("FEISHU_PERSONAL_EXACT_RECOVERY_KNOWLEDGE_IDS", owner.LogicalKnowledgeID)
			t.Setenv("FEISHU_PERSONAL_EXACT_RETRY", "1")
			policy, err := json.Marshal(map[string]int64{owner.LogicalKnowledgeID: sealed})
			require.NoError(t, err)
			t.Setenv("FEISHU_PERSONAL_EXACT_RETRY_GENERATIONS", string(policy))
			t.Setenv("FEISHU_PERSONAL_EXACT_RETRY_UNTIL", time.Now().Add(time.Minute).UTC().Format(time.RFC3339))
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = fmt.Fprint(w, `{"code":99991663,"msg":"invalid user access token"}`)
			}))
			t.Cleanup(server.Close)
			f.s.newClient = func(cfg *core.Config) *core.Client { cfg.BaseURL = server.URL; return core.NewClient(cfg) }
			ctx := types.WithPrincipal(personalUser(user),
				types.Principal{Type: types.PrincipalWebUser, ID: user})
			ctx = types.WithCaller(ctx, types.Caller{TenantID: 1, UserID: user, Role: role})
			before := exactPendingSnapshot(t, f, owner.ID)
			handled, accepted, err := f.ks.TryPersonalReparse(ctx, owner.LogicalKnowledgeID)
			require.True(t, handled)
			require.Nil(t, accepted)
			var app *apperrors.AppError
			require.ErrorAs(t, err, &app)
			require.Equal(t, 403, app.HTTPCode)
			if user == "unrelated" || identity == "viewer" {
				require.NotContains(t, fmt.Sprint(app.Details), "reauthorize_allowed")
			} else {
				require.Equal(t, map[string]any{
					"reason": "personal_exact_source_owner_reauthorization_required", "reauthorize_allowed": user == "A",
				}, app.Details)
			}
			require.JSONEq(t, string(before), string(exactPendingSnapshot(t, f, owner.ID)))
		})
	}
}
