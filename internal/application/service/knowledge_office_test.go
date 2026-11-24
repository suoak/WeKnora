package service

import (
	"os"
	"testing"

	werrors "github.com/Tencent/WeKnora/internal/errors"
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
	for _, name := range []string{"encrypted.docx", "encrypted.xlsx", "encrypted.xlsm", "encrypted.pptx", "encrypted.doc", "encrypted.xls", "encrypted.ppt", "disguised.txt"} {
		t.Run(name, func(t *testing.T) {
			repo := &createKnowledgeFileRepoStub{}
			files := &createKnowledgeFileServiceStub{}
			tasks := &createKnowledgeTaskEnqueuerStub{}
			svc := &knowledgeService{repo: repo, fileSvc: files, task: tasks, kbService: &createKnowledgeFileKBServiceStub{kb: &types.KnowledgeBase{ID: "kb-1"}}}
			_, err := svc.CreateKnowledgeFromFile(newCreateKnowledgeFileContext(), "kb-1", newMultipartFileHeader(t, name, officeFixture(t, name)), nil, nil, "renamed.txt", nil, "api", nil)
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
	h := newReplaceFileHarness(t)
	_, err := h.replace(t, officeFixture(t, "encrypted.docx"), "notes/replaced.txt", nil)
	var app *werrors.AppError
	require.ErrorAs(t, err, &app)
	require.Equal(t, werrors.ErrOfficeEncrypted, app.Code)
	require.Empty(t, h.events)
	require.Equal(t, h.original, h.repo.row)
}
