package core

import (
	"context"
	"fmt"
)

// FindDriveFileInFolder verifies a direct-child selection using the same
// authorized listing API as the picker. It stops at the matching page and
// never visits child folders or probes a document token as a folder.
func (c *Client) FindDriveFileInFolder(ctx context.Context, folderToken, fileToken string) (*DriveFile, error) {
	if fileToken == "" {
		return nil, fmt.Errorf("selected drive file token is required")
	}
	pageToken := ""
	seen := map[string]bool{}
	for {
		files, next, hasMore, err := c.listDriveFiles(ctx, folderToken, pageToken)
		if err != nil {
			return nil, err
		}
		for _, file := range files {
			if file.Token == fileToken {
				return &file, nil
			}
		}
		if !hasMore || next == "" {
			return nil, nil
		}
		if seen[next] {
			return nil, fmt.Errorf("feishu drive file pagination repeated page token %q", next)
		}
		seen[next] = true
		pageToken = next
	}
}
