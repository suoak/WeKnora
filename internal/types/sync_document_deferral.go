package types

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

const DocumentDeferralAwaitingAuthorization = "awaiting_document_owner_authorization"

// SyncDocumentDeferral names one exact source-owned document, never a title match.
// Removing the policy does not remove its durable pending cursor entry.
type SyncDocumentDeferral struct {
	TenantID         uint64    `json:"tenant_id"`
	DataSourceID     string    `json:"data_source_id"`
	KnowledgeBaseID  string    `json:"knowledge_base_id"`
	SourceResourceID string    `json:"source_resource_id"`
	ExternalID       string    `json:"external_id"`
	Title            string    `json:"title,omitempty"`
	ReasonCode       string    `json:"reason_code"`
	Reason           string    `json:"reason"`
	RequestedBy      string    `json:"requested_by"`
	RequestedAt      time.Time `json:"requested_at"`
	Reference        string    `json:"reference"`
}

func (d SyncDocumentDeferral) Key() string {
	// JSONB cannot store NUL even in escaped map keys. Use a domain-separated digest.
	sum := sha256.Sum256([]byte(d.SourceResourceID + "\x00" + d.ExternalID))
	return hex.EncodeToString(sum[:])
}

// ParseDocumentDeferrals fails closed on corrupt policy or cross-source copies.
func (d *DataSource) ParseDocumentDeferrals() ([]SyncDocumentDeferral, error) {
	var policy []SyncDocumentDeferral
	if len(d.DocumentDeferrals) == 0 {
		return policy, nil
	}
	if bytes.Equal(bytes.TrimSpace(d.DocumentDeferrals), []byte("null")) {
		return nil, errors.New("document deferral policy must be an array")
	}
	decoder := json.NewDecoder(bytes.NewReader(d.DocumentDeferrals))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return nil, errors.New("invalid document deferral policy")
	}
	if len(policy) > 100 {
		return nil, errors.New("too many document deferrals")
	}
	config, err := d.ParseConfig()
	if err != nil {
		return nil, err
	}
	roots := map[string]bool{}
	if config != nil {
		for _, root := range config.ResourceIDs {
			roots[root] = true
		}
	}
	seen := map[string]bool{}
	for _, entry := range policy {
		if entry.TenantID != d.TenantID || entry.DataSourceID != d.ID || entry.KnowledgeBaseID != d.KnowledgeBaseID ||
			!roots[entry.SourceResourceID] ||
			!validDeferralID(entry.SourceResourceID) ||
			!validDeferralID(entry.ExternalID) ||
			entry.ReasonCode != DocumentDeferralAwaitingAuthorization || strings.TrimSpace(entry.Reason) == "" ||
			len(entry.Reason) > 1024 ||
			len(entry.Title) > 1024 ||
			entry.RequestedBy == "" ||
			len(entry.RequestedBy) > 128 ||
			entry.RequestedAt.IsZero() ||
			entry.Reference == "" || len(entry.Reference) > 1024 || seen[entry.ExternalID] {
			return nil, errors.New("document deferral identity or audit record unconfirmed")
		}
		seen[entry.ExternalID] = true
	}
	return policy, nil
}

func validDeferralID(id string) bool {
	return id != "" && len(id) <= 512 && id == strings.TrimSpace(id) && !strings.ContainsAny(id, "\x00\r\n")
}

func DocumentDeferralHash(policy []SyncDocumentDeferral) string {
	if policy == nil {
		policy = []SyncDocumentDeferral{}
	}
	b, _ := json.Marshal(policy)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
