package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/Tencent/WeKnora/internal/application/service"
	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/types"
	"gorm.io/gorm"
)

func packagePartHashes(content []byte) (map[string]string, error) {
	z, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil || len(content) > 64<<20 || len(z.File) > 4096 {
		return nil, errors.New("invalid_probe_package")
	}
	parts := map[string]string{}
	var total int64
	for _, entry := range z.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		if entry.UncompressedSize64 > 32<<20 || parts[entry.Name] != "" {
			return nil, errors.New("probe_package_limit")
		}
		reader, err := entry.Open()
		if err != nil {
			return nil, err
		}
		body, readErr := io.ReadAll(io.LimitReader(reader, (32<<20)+1))
		_ = reader.Close()
		if readErr != nil || len(body) > 32<<20 {
			return nil, errors.New("probe_package_unreadable")
		}
		total += int64(len(body))
		if total > 128<<20 {
			return nil, errors.New("probe_package_limit")
		}
		digest := sha256.Sum256(body)
		parts[entry.Name] = hex.EncodeToString(digest[:])
	}
	return parts, nil
}

func inspectSheetPackages(
	ctx context.Context,
	db *gorm.DB,
	svc *service.FeishuPersonalService,
	ids []string,
) error {
	var item types.FeishuPersonalImport
	if err := db.Where("id IN ? AND resource_type = ?", ids, "sheet").First(&item).Error; err != nil {
		return err
	}
	owner := context.WithValue(ctx, types.UserIDContextKey, item.UserID)
	remote, client, ref, err := svc.ResolveCredentialReference(owner, types.FeishuUserOAuth, "")
	if err != nil || ref.OpenID != item.OpenID || ref.AppID != item.AppID {
		return errors.New("sheet_probe_identity_unconfirmed")
	}
	remote = core.WithApplicationTables(remote)
	resource, err := core.ParsePersonalResourceURL(item.URL)
	if err != nil {
		return err
	}
	resource, err = client.ResolvePersonalResource(remote, resource)
	if err != nil {
		return err
	}
	permission, err := client.InspectPersonalPermissions(remote, resource)
	if err != nil || !permission.View || !permission.Copy || !permission.Export {
		return errors.New("sheet_probe_permission_unconfirmed")
	}
	var previous map[string]string
	var rawHash string
	for n := 0; n < 2; n++ {
		content, _, err := client.DownloadPersonalResource(remote, resource)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(content)
		parts, err := packagePartHashes(content)
		if err != nil {
			return err
		}
		if n == 0 {
			previous, rawHash = parts, hex.EncodeToString(digest[:])
			continue
		}
		changed := []string{}
		for name, hash := range previous {
			if parts[name] != hash {
				category := "other_package_part"
				if name == "docProps/core.xml" {
					category = name
				} else if strings.HasPrefix(name, "xl/worksheets/") {
					category = "worksheet"
				}
				changed = append(changed, category)
			}
		}
		sort.Strings(changed)
		return json.NewEncoder(os.Stdout).Encode(map[string]any{
			"status": "CHECKED", "raw_bytes_equal": rawHash == hex.EncodeToString(digest[:]),
			"first_part_count": len(previous), "second_part_count": len(parts),
			"changed_part_categories": changed, "knowledge_writes": false,
		})
	}
	return nil
}
