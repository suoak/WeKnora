package access

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"reflect"
	"sort"

	"github.com/Tencent/WeKnora/internal/types"
)

type portalCandidateVersionKey struct{}

// WithPortalCandidateVersions opts only candidate searches into optional
// physical-version projection. Title, protocol and ordinary reads stay intact.
func WithPortalCandidateVersions(ctx context.Context) context.Context {
	return context.WithValue(ctx, portalCandidateVersionKey{}, true)
}

// PortalCandidateVersionsRequested reports whether this search needs row stamps.
func PortalCandidateVersionsRequested(ctx context.Context) bool {
	enabled, _ := ctx.Value(portalCandidateVersionKey{}).(bool)
	return enabled
}

// samePortalSnapshot excludes only optional physical row stamps used to avoid
// joining computations across credential/publication incarnations. A benign
// last-used write must not change the existing authorization contract.
func samePortalSnapshot(a, b *types.EnterpriseReadSnapshot) bool {
	if a == nil || b == nil {
		return a == b
	}
	x, y := *a, *b
	x.CandidateEpoch, y.CandidateEpoch = "", ""
	return reflect.DeepEqual(x, y)
}

// PortalCandidateIdentity exports only a hash of independently issued scope.
// No permission snapshot or grant crosses into another request. Missing
// physical stamps disables reuse (including SQLite and unsupported stores).
func PortalCandidateIdentity(ctx context.Context, targets []*PortalKBTarget) ([32]byte, bool) {
	var zero [32]byte
	if len(targets) == 0 || len(targets) > 128 {
		return zero, false
	}
	p, ok := ctx.Value(types.PrincipalContextKey).(types.Principal)
	c := types.CallerFromContext(ctx)
	k := normalizedReadCredential(ctx)
	if !ok || k == nil || k.KeyID == 0 || p.ID == "" || c.UserID != p.ID {
		return zero, false
	}
	type scope struct {
		Descriptor    PortalKBDescriptor
		Public        bool
		Documents     []string
		Snapshot      *types.EnterpriseReadSnapshot
		Epoch         string
		PrivateTenant uint64
	}
	ordered := append([]*PortalKBTarget(nil), targets...)
	for _, t := range ordered {
		if t == nil {
			return zero, false
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].descriptor.ID < ordered[j].descriptor.ID })
	h := sha256.New()
	encoder := json.NewEncoder(h)
	if encoder.Encode(struct {
		Principal types.Principal
		Caller    types.Caller
		Key       *types.TenantAPIKeyScope
	}{p, c, k}) != nil {
		return zero, false
	}
	for _, t := range ordered {
		if t.principal != p || !reflect.DeepEqual(t.caller, c) || !reflect.DeepEqual(t.credential, k) {
			return zero, false
		}
		s := t.privateSnapshot
		if t.public != nil {
			s = t.public.snapshot
		}
		if s == nil || s.CandidateEpoch == "" {
			return zero, false
		}
		docs := t.DocumentIDs()
		sort.Strings(docs)
		if encoder.Encode(scope{t.descriptor, t.Public(), docs, s, s.CandidateEpoch, t.privateTenant}) != nil {
			return zero, false
		}
	}
	copy(zero[:], h.Sum(nil))
	return zero, true
}
