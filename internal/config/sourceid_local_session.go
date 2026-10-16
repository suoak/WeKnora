package config

import (
	"errors"
	"strings"
	"time"
)

type SourceIDLocalPolicy struct {
	AccessTTL, AbsoluteTTL, IdleTTL, AdmissionMaxAge time.Duration
}

func (c *OIDCAuthConfig) EffectiveSourceIDSessionMode() string {
	if c.SourceIDSessionMode == "" {
		return "external"
	}
	return c.SourceIDSessionMode
}

// No production lifetime is implied by a default. Approval references and all
// durations must be explicitly supplied before this mode can issue a session.
func (c *OIDCAuthConfig) SourceIDLocalPolicy() (SourceIDLocalPolicy, error) {
	var p SourceIDLocalPolicy
	if c.EffectiveSourceIDSessionMode() != "local_approved" || !c.SourceIDEnable ||
		c.SourceIDProtocolOnly || !c.SourceIDContractConfirmed ||
		strings.TrimSpace(c.SourceIDLocalPolicyRef) == "" ||
		strings.TrimSpace(c.SourceIDAdmissionPolicyRef) == "" ||
		(c.SourceIDAdmissionMode != "authorization" && c.SourceIDAdmissionMode != "userinfo_claim" && c.SourceIDAdmissionMode != "sid_query") || c.SourceIDClaimsSource != "userinfo" ||
		c.SourceIDEmployeeClaim != "attributes.RJGH" || !c.SourceIDEmailBindingEnable || !c.SourceIDEmailContractConfirmed {
		return p, errors.New("sourceid local session policy pending")
	}
	for _, v := range []struct {
		value  string
		target *time.Duration
	}{
		{c.SourceIDLocalAccessTTL, &p.AccessTTL}, {c.SourceIDLocalAbsoluteTTL, &p.AbsoluteTTL},
		{c.SourceIDLocalIdleTTL, &p.IdleTTL}, {c.SourceIDAdmissionMaxAge, &p.AdmissionMaxAge},
	} {
		d, err := time.ParseDuration(v.value)
		if err != nil || d < time.Second || d%time.Second != 0 {
			return p, errors.New("sourceid local duration invalid")
		}
		*v.target = d
	}
	if p.AccessTTL > p.AbsoluteTTL || p.IdleTTL > p.AbsoluteTTL || p.AdmissionMaxAge > p.AbsoluteTTL {
		return p, errors.New("sourceid local duration exceeds absolute limit")
	}
	if c.SourceIDAdmissionMode == "authorization" && !c.SourceIDAuthorizationRecheckConfirmed {
		return p, errors.New("sourceid authorization admission contract pending")
	}
	if c.SourceIDAdmissionMode == "authorization" && !c.SourceIDForceReauthPromptLogin {
		return p, errors.New("sourceid forced reauthentication required")
	}
	if c.SourceIDAdmissionMode == "authorization" && c.SourceIDLocalRefreshEnable {
		return p, errors.New("sourceid authorization-only admission requires refresh disabled")
	}
	return p, nil
}
