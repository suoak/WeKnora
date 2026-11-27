package service

import (
	"context"
	"os"
	"testing"

	werrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/officeguard"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func officeFixture(t *testing.T, name string) string {
	t.Helper()
	p, e := os.ReadFile("../../officeguard/testdata/" + name)
	require.NoError(t, e)
	return string(p)
}

func TestOfficeCreateRejectsBeforeStorageDatabaseAndQueue(t *testing.T) {
	for _, name := range []string{
		"encrypted.docx", "encrypted.xlsx", "encrypted.xlsm", "encrypted.pptx",
		"encrypted.doc", "encrypted.xls", "encrypted.ppt", "disguised.txt",
		"classic-irm.doc", "classic-irm-viewer.doc", "classic-irm-disguised.txt",
	} {
		t.Run(name, func(t *testing.T) {
			repo := &createKnowledgeFileRepoStub{}
			files := &createKnowledgeFileServiceStub{}
			tasks := &createKnowledgeTaskEnqueuerStub{}
			svc := &knowledgeService{
				repo:      repo,
				fileSvc:   files,
				task:      tasks,
				kbService: &createKnowledgeFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb-1"}},
			}
			_, err := svc.CreateKnowledgeFromFile(
				newCreateKnowledgeFileContext(),
				"kb-1",
				newMultipartFileHeader(t, name, officeFixture(t, name)),
				nil,
				nil,
				"renamed.txt",
				nil,
				"api",
				nil,
			)
			var app *werrors.AppError
			require.ErrorAs(t, err, &app)
			require.Equal(t, werrors.ErrOfficeEncrypted, app.Code)
			require.Zero(t, files.saveCalls)
			require.Zero(t, repo.createCalls)
			require.Zero(t, tasks.calls)
		})
	}
}

func TestOfficeReplaceRejectsBeforeDequeueOrOldVersionMutation(t *testing.T) {
	for _, name := range []string{"encrypted.docx", "classic-irm.doc", "classic-irm-viewer.doc"} {
		t.Run(name, func(t *testing.T) {
			h := newReplaceFileHarness(t)
			_, err := h.replace(t, officeFixture(t, name), "notes/replaced.txt", nil)
			var app *werrors.AppError
			require.ErrorAs(t, err, &app)
			require.Equal(t, werrors.ErrOfficeEncrypted, app.Code)
			require.Empty(t, h.events)
			require.Equal(t, h.original, h.repo.row)
		})
	}
}

func TestOfficePlaceholderStopsParserResultAndRetry(t *testing.T) {
	const notice = "对此文档的权限当前受到限制。只有使用 Microsoft Office 2003 或更高版本才能打开该文档。" +
		"您可以向文档的作者申请一份可以使用用于 Internet Explorer 的权限管理加载项阅读的文档副本。"
	reader := feishuPDFTestReader(func(context.Context, *types.ReadRequest) (*types.ReadResult, error) {
		return &types.ReadResult{MarkdownContent: notice}, nil
	})
	s := &knowledgeService{}
	result, err := s.callDocReaderWithTimeout(context.Background(), reader, &types.ReadRequest{FileType: "doc"})
	require.Nil(t, result)
	require.True(t, officeguard.IsEncryptedError(err))
	code, action, permanent := classifyKnowledgeParseFailure(err)
	require.Equal(t, "encrypted_file", code)
	require.Equal(t, "provide_unencrypted_copy", action)
	require.True(t, permanent)
	_, message := knowledgeParseFailureState(context.Background(), err, false)
	require.Equal(t, officeguard.DRMMessage, message)
	refusal := feishuDocumentParserRefusal(&types.ReadResult{MarkdownContent: notice}, "doc")
	require.True(t, officeguard.IsEncryptedError(refusal))
}
