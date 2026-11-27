package officeguard

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"

	werrors "github.com/Tencent/WeKnora/internal/errors"
)

// Message is the controlled public explanation for opening-password encryption.
const Message = "该 Office 文件设置了打开密码，无法上传。请在 Office 或 WPS 中移除打开密码后重试。"

// DRMMessage explains rights management without suggesting an authorization bypass.
const DRMMessage = "该文件受 DRM 权限管理保护，知识库无法解析。请在有权限的原系统中导出未受保护的副本后再上传。"

// NewEncryptedError returns the shared typed 400/2400 opening-password refusal.
func NewEncryptedError() *werrors.AppError {
	return &werrors.AppError{Code: werrors.ErrOfficeEncrypted, Message: Message, HTTPCode: http.StatusBadRequest}
}

// IsEncryptedError recognizes the shared refusal code for opening encryption or DRM.
func IsEncryptedError(err error) bool {
	var app *werrors.AppError
	return errors.As(err, &app) && app.Code == werrors.ErrOfficeEncrypted
}

// RestrictionMessage retains the DRM explanation without exposing parser errors.
func RestrictionMessage(err error) string {
	var app *werrors.AppError
	if errors.As(err, &app) && app.Code == werrors.ErrOfficeEncrypted && app.Message == DRMMessage {
		return DRMMessage
	}
	return Message
}

// ValidateBytes applies the same bounded inspection to an in-memory source body.
func ValidateBytes(ctx context.Context, data []byte, name string) error {
	return Validate(ctx, bytes.NewReader(data), int64(len(data)), name)
}

// Validate rejects only confirmed opening encryption or DRM, and respects cancellation.
func Validate(ctx context.Context, r io.ReaderAt, size int64, name string) error {
	switch Detect(ctx, r, size, name) {
	case DRM:
		return &werrors.AppError{Code: werrors.ErrOfficeEncrypted, Message: DRMMessage, HTTPCode: http.StatusBadRequest}
	case Encrypted:
		return NewEncryptedError()
	}
	return ctx.Err()
}

// ValidateFile opens a separate handle and never changes the upload's read offset.
func ValidateFile(ctx context.Context, file *multipart.FileHeader) error {
	if file == nil {
		return werrors.NewBadRequestError("file is required")
	}
	f, err := file.Open()
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return Validate(ctx, f, file.Size, file.Filename)
}
