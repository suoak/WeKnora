package mcp

import (
	"bytes"
	"context"
	"errors"
	"testing"

	applog "github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/mark3labs/mcp-go/client/transport"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

type tokenStoreErrorRepo struct {
	interfaces.MCPOAuthRepository
	tenant    uint64
	principal types.Principal
	service   string
	writes    int
}

func (r *tokenStoreErrorRepo) GetTokenForPrincipal(_ context.Context, tenant uint64, principal types.Principal, service string) (*types.MCPOAuthToken, error) {
	r.tenant, r.principal, r.service = tenant, principal, service
	return nil, errors.New("SQL " + syntheticOAuthSensitive)
}
func (r *tokenStoreErrorRepo) SaveTokenForPrincipal(_ context.Context, row *types.MCPOAuthToken) error {
	r.tenant, r.principal, r.service = row.TenantID, types.Principal{Type: row.PrincipalType, ID: row.PrincipalID}, row.ServiceID
	r.writes++
	return errors.New("SQL " + syntheticOAuthSensitive)
}

func TestOAuthTokenStoreErrorsAreSafeAndScoped(t *testing.T) {
	for _, mode := range []string{"read", "managed-read", "write", "cancel-read", "cancel-write"} {
		t.Run(mode, func(t *testing.T) {
			principal := types.Principal{Type: types.PrincipalWebUser, ID: "same-principal"}
			repo := &tokenStoreErrorRepo{}
			store := newDBTokenStore(repo, 8, principal, "builtin")
			var captured bytes.Buffer
			l := logrus.New()
			l.SetOutput(&captured)
			ctx := context.WithValue(context.Background(), types.LoggerContextKey, logrus.NewEntry(l))
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()
			if mode == "cancel-read" || mode == "cancel-write" {
				cancel()
			}
			var err error
			switch mode {
			case "read", "cancel-read":
				_, err = store.GetToken(ctx)
			case "managed-read":
				_, err = newManagedTokenStore(repo, 8, principal, "builtin").GetToken(ctx)
			default:
				err = store.SaveToken(ctx, &transport.Token{AccessToken: "synthetic-access-token", RefreshToken: "synthetic-refresh-token"})
			}
			require.Error(t, err)
			if mode == "cancel-read" || mode == "cancel-write" {
				require.ErrorIs(t, err, context.Canceled)
				require.Zero(t, repo.writes)
				require.Zero(t, repo.tenant)
			} else {
				require.EqualValues(t, 8, repo.tenant)
				require.Equal(t, principal, repo.principal)
				require.Equal(t, "builtin", repo.service)
				var safe *OAuthSafeError
				require.ErrorAs(t, err, &safe)
				code := "mcp_oauth_token_read_failed"
				if mode == "write" {
					code = "mcp_oauth_token_write_failed"
					require.Equal(t, 1, repo.writes)
				}
				require.Equal(t, code, safe.Code)
			}
			applog.GetLogger(ctx).Errorf("token store: %v", err)
			for _, s := range []string{"synthetic-client-secret", "synthetic-access-token", "synthetic-refresh-token", "synthetic-password"} {
				require.NotContains(t, err.Error(), s)
				require.NotContains(t, SafeOAuthErrorMessage(err), s)
				require.NotContains(t, captured.String(), s)
			}
		})
	}
}
