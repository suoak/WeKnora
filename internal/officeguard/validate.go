package officeguard

import (
	"bytes"
	"context"
	"errors"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"io"
	"mime/multipart"
	"net/http"
)

const Message = "该 Office 文件设置了打开密码，无法上传。请在 Office 或 WPS 中移除打开密码后重试。"

func NewEncryptedError() *werrors.AppError {
	return &werrors.AppError{Code: werrors.ErrOfficeEncrypted, Message: Message, HTTPCode: http.StatusBadRequest}
}

func IsEncryptedError(err error) bool {
	var app *werrors.AppError
	return errors.As(err, &app) && app.Code == werrors.ErrOfficeEncrypted
}

func ValidateBytes(ctx context.Context, data []byte, name string) error {
	return Validate(ctx, bytes.NewReader(data), int64(len(data)), name)
}

func Validate(ctx context.Context, r io.ReaderAt, size int64, name string) error {
	if Detect(ctx, r, size, name) == Encrypted {
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
	defer f.Close()
	return Validate(ctx, f, file.Size, file.Filename)
}
