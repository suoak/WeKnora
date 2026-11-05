package service

import (
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestFeishuSourceBindingHealthDistinguishesConsentFromAuthorization(t *testing.T) {
	for _, tc := range []struct {
		cause error
		code  string
	}{
		{ErrFeishuReconnect, "reauthorize"},
		{ErrFeishuSharingApproval, "verification_required"},
		{ErrFeishuSourceRebindRequired, "verification_required"},
		{access.ErrForbidden, "permission_denied"},
		{errors.New("unknown binding fault"), "verification_required"},
	} {
		ds := types.DataSource{
			Type: "feishu", LegacyAccessState: "hold",
			ErrorMessage: feishuSourceBindingFailureMessage(tc.cause),
		}
		require.Equal(t, tc.code, ds.DescribeSyncHealth().Code)
	}
}
