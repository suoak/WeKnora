package officeguard

import (
	"context"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"io"
	"mime/multipart"
	"net/http"
)

const Message = "该 Office 文件设置了打开密码，无法上传。请在 Office 或 WPS 中移除打开密码后重试。"

func Validate(ctx context.Context, r io.ReaderAt, size int64, name string) error {
	if Detect(ctx, r, size, name) == Encrypted {
		return &werrors.AppError{Code: werrors.ErrOfficeEncrypted, Message: Message, HTTPCode: http.StatusBadRequest}
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
