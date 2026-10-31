package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestExactPersonalPreflightDiagnosticsNeverFormatSecrets(t *testing.T) {
	owner := &types.FeishuPersonalAttempt{
		ID: "attempt", LogicalKnowledgeID: "knowledge", Fence: 3,
		OwnerNonce: "secret-nonce", ManifestKey: "/private/secret-manifest",
	}
	api := &core.APIError{
		Category: core.ErrorCategory("secret-category"), Operation: "secret-operation",
		Code: 1069902, HTTPStatus: 403, Message: "secret-provider-message",
		RequestID: "secret-request-id", ProviderReason: "secret-provider-reason",
		Cause: errors.New("SSRF blocked https://private.invalid/?token=secret-token"),
	}
	fields := exactPersonalPreflightFields(owner, "remote_permissions", &[3]bool{true, false, true},
		fmt.Errorf("secret-prefix: %w", api))
	data, err := json.Marshal(fields)
	require.NoError(t, err)
	require.NotContains(t, string(data), "secret")
	require.NotContains(t, string(data), "private.invalid")
	require.Equal(t, "other", fields["provider_operation"])
	require.Equal(t, "security_rejected", fields["transport_class"])
	require.Equal(t, false, fields["export_allowed"])
	require.Equal(t, int64(3), fields["generation"])

	missing := exactPersonalPreflightFields(owner, "file_registration", nil,
		fmt.Errorf("secret-path: %w", gorm.ErrRecordNotFound))
	require.Equal(t, "authoritative_record_missing", missing["failure_class"])
}
