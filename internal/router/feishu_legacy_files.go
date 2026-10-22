package router

import (
	"context"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
)

type legacyFileGate interface {
	CheckLegacyFileRead(context.Context, uint64, string) (bool, error)
}

func legacyFilePreflight(c *gin.Context, checker any, tenant uint64, path string) (bool, bool) {
	gate, ok := checker.(legacyFileGate)
	if !ok {
		return false, true
	}
	managed, err := gate.CheckLegacyFileRead(c.Request.Context(), tenant, path)
	if err != nil {
		c.Header("Cache-Control", "private, no-store")
		c.AbortWithStatus(http.StatusForbidden)
		return managed, false
	}
	return managed, true
}

type legacyFileReader struct {
	io.ReadCloser
	ctx    context.Context
	gate   legacyFileGate
	tenant uint64
	path   string
}

func (r *legacyFileReader) Read(b []byte) (int, error) {
	if _, e := r.gate.CheckLegacyFileRead(r.ctx, r.tenant, r.path); e != nil {
		return 0, e
	}
	return r.ReadCloser.Read(b)
}

type legacySeekFileReader struct {
	*legacyFileReader
	seeker io.Seeker
}

func (r *legacySeekFileReader) Seek(offset int64, whence int) (int64, error) {
	return r.seeker.Seek(offset, whence)
}
func guardLegacyFileReader(c *gin.Context, checker any, tenant uint64, path string, reader io.ReadCloser, managed bool) io.ReadCloser {
	gate, ok := checker.(legacyFileGate)
	if !ok {
		return reader
	}
	wrapped := &legacyFileReader{ReadCloser: reader, ctx: c.Request.Context(), gate: gate, tenant: tenant, path: path}
	if seek, ok := reader.(io.Seeker); ok {
		return &legacySeekFileReader{legacyFileReader: wrapped, seeker: seek}
	}
	return wrapped
}
