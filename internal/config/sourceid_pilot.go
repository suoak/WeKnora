package config

import (
	"encoding/json"
	"errors"
	"github.com/Tencent/WeKnora/internal/types"
	"io"
	"os"
	"strings"
)

// This protected manifest is account-ownership evidence supplied by an
// authorized operator, not employee data inferred from a successful token.
// It is never logged, serialized into config APIs, or stored in a session.
// Emails are needed only for first association; approved identities keep using
// their original user ID even if their email later changes.
func (c *OIDCAuthConfig) SourceIDPilotAccounts() (map[string]types.SourceIDPilotAccount, error) {
	fail := errors.New("sourceid pilot ownership unavailable")
	if c.SourceIDAdmissionMode != "oidc_sso_approved" || !c.SourceIDSSOPolicyConfirmed {
		return nil, fail
	}
	info, err := os.Lstat(c.SourceIDPilotAccountsFile)
	if err != nil || !info.Mode().IsRegular() || !privateSourceIDPilotFile(c.SourceIDPilotAccountsFile, info) || info.Size() > 65536 {
		return nil, fail
	}
	f, err := os.Open(c.SourceIDPilotAccountsFile)
	if err != nil {
		return nil, fail
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !os.SameFile(info, opened) {
		return nil, fail
	}
	var manifest struct {
		Version      int    `json:"version"`
		PolicyRef    string `json:"policy_ref"`
		OwnershipRef string `json:"ownership_ref"`
		Issuer       string `json:"issuer"`
		Accounts     []struct {
			UserID     string `json:"user_id"`
			Email      string `json:"email"`
			EmployeeNo string `json:"employee_no"`
		} `json:"accounts"`
	}
	d := json.NewDecoder(io.LimitReader(f, 65537))
	d.DisallowUnknownFields()
	if d.Decode(&manifest) != nil || d.Decode(new(interface{})) != io.EOF || manifest.Version != 1 ||
		manifest.PolicyRef != c.SourceIDAutoBindingPolicyRef || strings.TrimSpace(manifest.OwnershipRef) == "" || manifest.Issuer == "" || manifest.Issuer != c.IssuerURL ||
		len(manifest.Accounts) == 0 || len(manifest.Accounts) > 100 {
		return nil, fail
	}
	accounts, emails, employees := map[string]types.SourceIDPilotAccount{}, map[string]bool{}, map[string]bool{}
	for _, a := range manifest.Accounts {
		email := strings.ToLower(strings.TrimSpace(a.Email))
		if a.UserID == "" || strings.TrimSpace(a.UserID) != a.UserID || len(a.UserID) > 128 ||
			strings.Count(email, "@") != 1 || strings.HasPrefix(email, "@") || strings.HasSuffix(email, "@") || emails[email] || accounts[a.UserID].Email != "" ||
			a.EmployeeNo == "" || strings.TrimSpace(a.EmployeeNo) != a.EmployeeNo || len(a.EmployeeNo) > 256 || employees[a.EmployeeNo] {
			return nil, fail
		}
		for _, ch := range email {
			if ch <= 32 || ch > 126 {
				return nil, fail
			}
		}
		accounts[a.UserID], emails[email], employees[a.EmployeeNo] = types.SourceIDPilotAccount{Email: email, EmployeeNo: a.EmployeeNo}, true, true
	}
	return accounts, nil
}
