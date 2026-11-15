package service

import (
	"context"
	"strings"
	"testing"

	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestSourceDocumentReparseRequiresLivePrincipalBeforeLookups(t *testing.T) {
	service := &DataSourceService{}
	for _, principal := range []types.Principal{
		{}, {Type: types.PrincipalWebUser, ID: "different-user"},
	} {
		ctx := types.WithCaller(context.Background(), types.Caller{
			TenantID: 7, UserID: "requester", Role: types.TenantRoleAdmin,
		})
		ctx = context.WithValue(ctx, types.PrincipalContextKey, principal)
		_, err := service.RequestDocumentReparse(ctx, "source", "document", nil)
		var app *apperrors.AppError
		require.ErrorAs(t, err, &app)
		require.Equal(t, 403, app.HTTPCode)
	}
}

func TestSourceDocumentReparseConfigurationCannotSilentlyDropUnsupportedActions(t *testing.T) {
	flag := false
	kb := &types.KnowledgeBase{Type: "document"}
	for _, config := range []*types.KnowledgeProcessOverrides{
		{SummaryEnabled: &flag},
		{GraphEnabled: &flag},
		{ASRConfig: &types.ASRConfig{}},
		{ImageAttrsEnabled: &flag},
	} {
		err := validateSourceDocumentReparseConfig(context.Background(), kb, "docx", config)
		var app *apperrors.AppError
		require.ErrorAs(t, err, &app)
		require.Equal(t, 400, app.HTTPCode)
	}
	require.NoError(t, validateSourceDocumentReparseConfig(context.Background(), kb, "docx", nil))
	config := &types.KnowledgeProcessOverrides{ParserEngineRules: []types.ParserEngineRule{
		{FileTypes: []string{"docx"}, Engine: "anydoc"},
	}}
	require.NoError(t, validateSourceDocumentReparseConfig(context.Background(), kb, "docx", config))
}

func TestSourceDocumentReparseRecoveryRegistrationIsIndependentAndIdempotent(t *testing.T) {
	h := NewHousekeepingService(nil, nil, nil, nil)
	before := len(h.cron.Entries())
	callback := func(context.Context) error { return nil }
	require.NoError(t, h.SetSourceDocumentReparseRecovery(callback))
	require.NoError(t, h.SetSourceDocumentReparseRecovery(callback))
	require.Len(t, h.cron.Entries(), before+1, "source recovery must use its own cron job, not the generic sweep")
}

func TestSourceDocumentReparseParserDiagnosisDoesNotExposeRawOutput(t *testing.T) {
	t.Setenv("FEISHU_SAFE_INCREMENTAL_ENABLED", "true")
	ds := &types.DataSource{Type: "feishu_drive"}
	for _, tc := range []struct{ message, code string }{
		{"file is encrypted; private/source.xlsx", "safe_staging_encrypted_file"},
		{"resource limit; private/source.xlsx", "safe_staging_parser_resource_limit"},
		{"not a zip file; private/source.xlsx", "safe_staging_file_format_mismatch"},
	} {
		err := feishuDocumentParserRefusal(&types.ReadResult{Error: tc.message}, "xlsx")
		require.Error(t, err)
		require.False(t, strings.Contains(err.Error(), "private/"))
		require.Equal(t, tc.code, feishuDocumentFailureCode(ds, &types.FetchedItem{ExternalID: "doc"}, err))
	}
	err := feishuDocumentParserRefusal(&types.ReadResult{Error: "unknown private/source.xlsx"}, "xlsx")
	require.Empty(t, feishuDocumentFailureCode(ds, &types.FetchedItem{ExternalID: "doc"}, err),
		"unknown failures must not be labelled encrypted or permanently corrupt")
}
