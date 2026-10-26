package core

import (
	"context"
	"net/http"
	"net/url"
)

// PersonalExportPolicy reads the documented sharing security setting under the
// same personal identity. Failure never grants permission or bypasses export.
func (c *Client) PersonalExportPolicy(ctx context.Context, resource PersonalResource) (string, error) {
	if _, ok := ctx.Value(userIdentityKey{}).(userIdentity); !ok {
		return "", ErrAuth
	}
	if _, app := ctx.Value(applicationIdentityKey{}).(applicationIdentity); app {
		return "", ErrAuth
	}
	if !resourceTokenPattern.MatchString(resource.Token) {
		return "", ErrPermanent
	}
	var response struct {
		Data struct {
			Permission struct {
				SecurityEntity string `json:"security_entity"`
			} `json:"permission_public"`
		} `json:"data"`
	}
	err := c.doRequest(
		ctx,
		"get_export_policy",
		http.MethodGet,
		"/open-apis/drive/v1/permissions/"+url.PathEscape(
			resource.Token,
		)+"/public?type="+url.QueryEscape(
			resource.Type,
		),
		nil,
		&response,
	)
	if err != nil {
		return "", err
	}
	value := response.Data.Permission.SecurityEntity
	switch value {
	case "anyone_can_view", "anyone_can_edit", "only_full_access":
		return value, nil
	}
	return "unknown", nil
}
