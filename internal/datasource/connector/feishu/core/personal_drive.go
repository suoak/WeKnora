package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// PersonalDrivePage is a bounded, user-authorized directory page, not an
// enterprise-wide search or a promise to enumerate every shared document.
type PersonalDrivePage struct {
	Items    []PersonalDriveItem `json:"items"`
	HasMore  bool                `json:"has_more"`
	NextPage string              `json:"next_page_token"`
}

type PersonalDriveItem struct {
	Token      string `json:"token"`
	Name       string `json:"name"`
	Type       string `json:"type"`
	URL        string `json:"url"`
	Modified   string `json:"modified"`
	Importable bool   `json:"importable"`
}

var ErrPersonalDrivePage = errors.New("feishu_invalid_drive_page")

func validPersonalDriveCursor(page string) bool {
	return len(page) <= 2048 && strings.IndexFunc(page, func(r rune) bool { return r < 32 || r == 127 }) < 0
}

func (c *Client) ListPersonalDrivePage(ctx context.Context, folder, page string) (*PersonalDrivePage, error) {
	if _, ok := ctx.Value(userIdentityKey{}).(userIdentity); !ok {
		return nil, ErrAuth
	}
	if (folder != "" && !resourceTokenPattern.MatchString(folder)) || !validPersonalDriveCursor(page) {
		return nil, ErrPersonalDrivePage
	}
	q := url.Values{"page_size": {"50"}, "order_by": {"EditedTime"}, "direction": {"DESC"}}
	if folder != "" {
		q.Set("folder_token", folder)
	}
	if page != "" {
		q.Set("page_token", page)
	}
	body, err := c.doRequestBytes(ctx, "list_personal_drive_page", http.MethodGet, "/open-apis/drive/v1/files?"+q.Encode(), nil, true, false, 2*1024*1024)
	if err != nil {
		return nil, err
	}
	var response struct {
		Data *DriveFileListData `json:"data"`
	}
	if json.Unmarshal(body, &response) != nil || response.Data == nil {
		return nil, ErrPersonalDrivePage
	}
	d := *response.Data
	if len(d.Files) > 200 || (d.HasMore && (d.NextPageToken == "" || d.NextPageToken == page || !validPersonalDriveCursor(d.NextPageToken))) {
		return nil, ErrPersonalDrivePage
	}
	result := &PersonalDrivePage{Items: []PersonalDriveItem{}, HasMore: d.HasMore}
	if d.HasMore {
		result.NextPage = d.NextPageToken
	}
	for _, f := range d.Files {
		if !resourceTokenPattern.MatchString(f.Token) {
			return nil, ErrPersonalDrivePage
		}
		item := PersonalDriveItem{Token: f.Token, Name: f.Name, Type: f.Type, Modified: f.ModifiedTime}
		// Never hand a provider-controlled redirect or active URL to the UI.
		if r, err := ParsePersonalResourceURL(f.URL); err == nil && r.Token == f.Token && r.Type == f.Type {
			item.URL = r.URL
			item.Importable = r.Type != "mindnote"
		}
		result.Items = append(result.Items, item)
	}
	return result, nil
}
