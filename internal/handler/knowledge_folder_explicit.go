package handler

import (
	"github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
	"net/http"
	"strings"
)

// CreateKnowledgeFolderRequest creates one empty child of ParentPath.
type CreateKnowledgeFolderRequest struct {
	// ParentPath is an existing canonical parent folder path; empty or omitted creates a child at the knowledge
	// base root.
	ParentPath string `json:"parent_path" extensions:"x-agent"`
	// Name is one folder-name segment under ParentPath, without slash separators; an existing sibling with the
	// same name is rejected.
	Name string `json:"name" binding:"required" extensions:"x-agent"`
}

// CreateKnowledgeFolder godoc
// @Summary      新建知识库文件夹
// @Description  在知识库根目录或现有目录下创建空文件夹，无需上传或移动文档
// @Tags         知识管理
// @Accept       json
// @Produce      json
// @Param        id       path      string                        true  "知识库ID"  extensions(x-agent=knowledge_base_id)
// @Param        request  body      CreateKnowledgeFolderRequest  true  "新建请求"
// @Success      201      {object}  handler.DataResponse{data=types.KnowledgeFolderNode}  "创建成功"
// @Failure      400      {object}  errors.AppError  "名称或路径无效"
// @Failure      403      {object}  errors.AppError  "权限不足"
// @Failure      404      {object}  errors.AppError  "父目录不存在"
// @Failure      409      {object}  errors.AppError  "同级文件夹已存在"
// @Failure      500      {object}  errors.AppError  "创建失败"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/knowledge/folders [post]
func (h *KnowledgeHandler) CreateKnowledgeFolder(c *gin.Context) {
	var req CreateKnowledgeFolderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		_ = c.Error(errors.NewBadRequestError("Invalid request parameters: " + err.Error()))
		return
	}

	kbID, effectiveTenantID, err := h.requireKnowledgeWriteAccess(c, c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	ctx := types.WithExecutionTenant(c.Request.Context(), effectiveTenantID)
	folder, err := h.kgService.CreateKnowledgeFolder(ctx, kbID, req.ParentPath, req.Name)
	if err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			_ = c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		_ = c.Error(errors.NewInternalServerError("Failed to create folder"))
		return
	}
	c.JSON(http.StatusCreated, gin.H{"success": true, "data": folder})
}

// DeleteKnowledgeFolderResponse identifies the empty folder that was deleted.
type DeleteKnowledgeFolderResponse struct {
	// Path is the canonical path of the deleted empty folder; no documents or descendant folders were deleted.
	Path string `json:"path" extensions:"x-agent=folder_path"`
}

// DeleteKnowledgeFolder godoc
// @Summary      删除知识库空文件夹
// @Description  仅删除没有文档及子目录的文件夹，不删除文档
// @Tags         知识管理
// @Produce      json
// @Param        id    path   string  true  "知识库ID"  extensions(x-agent=knowledge_base_id)
// @Param        path  query  string  true  "文件夹路径"  extensions(x-agent=folder_path)
// @Success      200  {object}  handler.DataResponse{data=DeleteKnowledgeFolderResponse}  "删除成功"
// @Failure      400  {object}  errors.AppError  "路径无效"
// @Failure      403  {object}  errors.AppError  "权限不足"
// @Failure      404  {object}  errors.AppError  "文件夹不存在"
// @Failure      409  {object}  errors.AppError  "文件夹非空"
// @Failure      500  {object}  errors.AppError  "删除失败"
// @Security     Bearer
// @Security     ApiKeyAuth
// @Router       /knowledge-bases/{id}/knowledge/folders/empty [delete]
func (h *KnowledgeHandler) DeleteKnowledgeFolder(c *gin.Context) {
	path, ok := c.GetQuery("path")
	if !ok || strings.TrimSpace(path) == "" {
		_ = c.Error(errors.NewBadRequestError("Folder path is required"))
		return
	}
	kbID, effectiveTenantID, err := h.requireKnowledgeWriteAccess(c, c.Param("id"))
	if err != nil {
		_ = c.Error(err)
		return
	}
	ctx := types.WithExecutionTenant(c.Request.Context(), effectiveTenantID)
	if err := h.kgService.DeleteKnowledgeFolder(ctx, kbID, path); err != nil {
		if appErr, ok := errors.IsAppError(err); ok {
			_ = c.Error(appErr)
			return
		}
		logger.ErrorWithFields(ctx, err, nil)
		_ = c.Error(errors.NewInternalServerError("Failed to delete folder"))
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": DeleteKnowledgeFolderResponse{Path: path}})
}
