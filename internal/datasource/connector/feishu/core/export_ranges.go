package core

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

const feishuExportDownloadPartBytes int64 = 8 * 1024 * 1024

type requestByteOptions struct {
	headers      http.Header
	partialLimit int64
	validate     func(*http.Response, []byte) error
}

func invalidExportRange() error {
	return &APIError{Category: ErrorCategoryPermanent, Operation: "download_export_file_range_invalid"}
}

func parseExportContentRange(value string) (start, end, total int64, err error) {
	parts := strings.Split(strings.TrimPrefix(value, "bytes "), "/")
	if !strings.HasPrefix(value, "bytes ") || len(parts) != 2 {
		return 0, 0, 0, invalidExportRange()
	}
	span := strings.Split(parts[0], "-")
	if len(span) != 2 {
		return 0, 0, 0, invalidExportRange()
	}
	values := []*int64{&start, &end, &total}
	for i, raw := range []string{span[0], span[1], parts[1]} {
		*values[i], err = strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return 0, 0, 0, invalidExportRange()
		}
	}
	if start < 0 || end < start || total <= end || total > maxFeishuDownloadBytes {
		return 0, 0, 0, invalidExportRange()
	}
	return start, end, total, nil
}

// One immutable export file token is used for every part. All parts pass through
// the existing identity revalidation, SSRF transport, limiter, finite retries and
// unchanged per-request deadline. Nothing is emitted until assembly is complete.
func (c *Client) downloadExportRanges(ctx context.Context, path string, partBytes int64) ([]byte, error) {
	if partBytes <= 0 || partBytes > feishuExportDownloadPartBytes {
		return nil, invalidExportRange()
	}
	var data []byte
	var total int64
	var etag, modified string
	maxParts := (maxFeishuDownloadBytes + partBytes - 1) / partBytes
	for part := int64(0); part < maxParts; part++ {
		start := int64(len(data))
		requestedEnd := start + partBytes - 1
		if total > 0 {
			requestedEnd = min(requestedEnd, total-1)
		}
		headers := http.Header{
			"Range":           []string{fmt.Sprintf("bytes=%d-%d", start, requestedEnd)},
			"Accept-Encoding": []string{"identity"},
		}
		if etag != "" && !strings.HasPrefix(etag, "W/") {
			headers.Set("If-Match", etag)
		} else if modified != "" {
			headers.Set("If-Unmodified-Since", modified)
		}
		wholeResponse := false
		options := &requestByteOptions{headers: headers, partialLimit: partBytes}
		options.validate = func(response *http.Response, body []byte) error {
			if response.StatusCode == http.StatusOK && start == 0 {
				// Compatibility: a server may ignore Range and return the full file.
				wholeResponse = true
				return nil
			}
			if response.StatusCode != http.StatusPartialContent ||
				(response.Header.Get("Content-Encoding") != "" &&
					response.Header.Get("Content-Encoding") != "identity") {
				return invalidExportRange()
			}
			first, last, size, err := parseExportContentRange(response.Header.Get("Content-Range"))
			if err != nil || first != start || last != min(requestedEnd, size-1) || int64(len(body)) != last-first+1 {
				return invalidExportRange()
			}
			if part == 0 {
				total, etag, modified = size, response.Header.Get("ETag"), response.Header.Get("Last-Modified")
			} else if size != total || response.Header.Get("ETag") != etag ||
				response.Header.Get("Last-Modified") != modified {
				return invalidExportRange()
			}
			return nil
		}
		body, err := c.doRequestBytesWithOptions(ctx, "download_export_file", http.MethodGet, path,
			nil, true, true, maxFeishuDownloadBytes, options)
		if err != nil {
			return nil, err
		}
		if wholeResponse {
			return body, nil
		}
		data = append(data, body...)
		if int64(len(data)) == total {
			return data, nil
		}
	}
	return nil, invalidExportRange()
}
