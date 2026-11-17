package confluence

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

const defaultBuiltinURL = "http://conf.ruijie.work"

// BuiltinProfileInfo is the public surface of the system account. Identity and
// authentication values never enter connector metadata or a data-source row.
func BuiltinProfileInfo() types.DataSourceSystemAccountInfo {
	cfg, err := builtinConfig()
	if err != nil {
		return types.DataSourceSystemAccountInfo{}
	}
	return types.DataSourceSystemAccountInfo{Enabled: true, BaseURL: cfg.baseURL}
}

func systemAccountRedirectPolicy(baseURL string, previous func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	base, _ := url.Parse(baseURL)
	return func(req *http.Request, via []*http.Request) error {
		if req.URL.User != nil || req.URL.Scheme != base.Scheme || !strings.EqualFold(req.URL.Host, base.Host) {
			return fmt.Errorf("system Confluence redirect must stay on the configured origin")
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("too many Confluence redirects")
		}
		return nil
	}
}

func builtinConfig() (config, error) {
	unavailable := fmt.Errorf("system Confluence account is unavailable; contact the system administrator")
	enabled, err := strconv.ParseBool(strings.TrimSpace(os.Getenv("CONFLUENCE_BUILTIN_ENABLED")))
	if err != nil || !enabled {
		return config{}, unavailable
	}
	baseURL := strings.TrimSpace(os.Getenv("CONFLUENCE_BUILTIN_BASE_URL"))
	if baseURL == "" {
		baseURL = defaultBuiltinURL
	}
	baseURL, err = normalizeBaseURL(baseURL, false)
	if err != nil {
		return config{}, unavailable
	}
	u, err := url.Parse(baseURL)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return config{}, unavailable
	}
	username := strings.TrimSpace(os.Getenv("CONFLUENCE_BUILTIN_USERNAME"))
	password := os.Getenv("CONFLUENCE_BUILTIN_PASSWORD")
	if path := strings.TrimSpace(os.Getenv("CONFLUENCE_BUILTIN_PASSWORD_FILE")); path != "" {
		file, err := os.Open(path)
		if err != nil {
			return config{}, unavailable
		}
		defer file.Close()
		raw, err := io.ReadAll(io.LimitReader(file, 16385))
		if err != nil || len(raw) > 16384 {
			return config{}, unavailable
		}
		password = strings.TrimRight(string(raw), "\r\n")
	}
	if username == "" || password == "" {
		return config{}, unavailable
	}
	return config{edition: editionServer, baseURL: baseURL, username: username, secret: password, systemAccount: true}, nil
}
