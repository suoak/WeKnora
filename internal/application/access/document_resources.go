package access

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
)

// DerivedText is an in-memory provenance proof, created only by a generator
// which has consumed authorized sources. Historical Wiki/FAQ/graph/cache rows
// have no such proof and cannot be upgraded by supplying a list of IDs.
type DerivedText struct {
	body    string
	digest  [32]byte
	sources []*DocumentReadGrant
}

// BuildDerivedText retains ALL input dependencies, conservatively, even if the
// generated text uses only a subset. Producers must use the canonical reads
// under these grants, never an unscoped cache or historical index body.
func BuildDerivedText(ctx context.Context, sources []*DocumentReadGrant, chunks EnterpriseTextChunks, produce func([]EnterpriseParsedChunk) (string, error)) (*DerivedText, error) {
	if len(sources) == 0 || len(sources) > MaxDocumentAllowSet || produce == nil || chunks == nil {
		return nil, ErrForbidden
	}
	inputs := []EnterpriseParsedChunk{}
	for _, g := range sources {
		if g == nil || g.reader == nil || g.operation != "read" {
			return nil, ErrForbidden
		}
		rows, err := g.reader.ReadText(ctx, g, chunks)
		if err != nil {
			return nil, err
		}
		inputs = append(inputs, rows...)
	}
	for _, g := range sources {
		if err := g.Revalidate(ctx); err != nil {
			return nil, err
		}
	}
	body, err := produce(inputs)
	if err != nil {
		return nil, err
	}
	proof := &DerivedText{body: body, digest: sha256.Sum256([]byte(body)), sources: append([]*DocumentReadGrant(nil), sources...)}
	if _, err = proof.Read(ctx); err != nil {
		return nil, err
	}
	return proof, nil
}

func (d *DerivedText) Read(ctx context.Context) (string, error) {
	if d == nil || len(d.sources) == 0 || sha256.Sum256([]byte(d.body)) != d.digest || !safeEnterpriseText(d.body) {
		return "", ErrForbidden
	}
	for _, g := range d.sources {
		if err := g.Revalidate(ctx); err != nil {
			return "", err
		}
	}
	return d.body, nil
}

// ControlledResource contains only a server-loaded storage reference. A URL or
// client-provided path is never a reference. Containment proof is evaluated by
// the mandatory live File base verifier; it is not a boolean supplied by users.
type ControlledResource struct {
	ID               string
	TenantID         uint64
	KBID, DocumentID string
	StorageReference string
	ContentSHA256    string
	DocumentVersion  string
}
type ControlledResourceStore interface {
	LoadControlledResource(context.Context, string) (*ControlledResource, error)
	OpenControlledResource(context.Context, *ControlledResource) (io.ReadCloser, error)
}

// ResourceBroker buffers a bounded file, checking live authorization before
// opening it and again before any bytes leave this component. It never signs a
// bearer URL or redirects to object storage. The caller must use private,
// no-store HTTP delivery and must not expose the storage reference.
type ResourceBroker struct {
	enabled bool
	reader  *DocumentReader
	store   ControlledResourceStore
}

func NewResourceBroker(enabled bool, reader *DocumentReader, store ControlledResourceStore) *ResourceBroker {
	return &ResourceBroker{enabled: enabled, reader: reader, store: store}
}

const MaxControlledResourceBytes = 8 << 20

func (b *ResourceBroker) Read(ctx context.Context, id string) ([]byte, error) {
	if b == nil || !b.enabled || b.reader == nil || b.store == nil || id == "" {
		return nil, ErrForbidden
	}
	ref, err := b.store.LoadControlledResource(ctx, id)
	if err != nil {
		return nil, err
	}
	if ref == nil || ref.ID != id || ref.TenantID == 0 || ref.StorageReference == "" || len(ref.ContentSHA256) != 64 || ref.DocumentVersion == "" {
		return nil, ErrForbidden
	}
	bound := *ref
	g, err := b.reader.Authorize(ctx, ref.KBID, ref.DocumentID, "file")
	if err != nil {
		return nil, err
	}
	if g.tenant != ref.TenantID {
		return nil, ErrForbidden
	}
	versionMatches := false
	for _, d := range g.snapshot.Documents {
		if d.ID == g.document && d.CurrentVersion == bound.DocumentVersion {
			versionMatches = true
		}
	}
	if !versionMatches {
		return nil, ErrForbidden
	}
	file, err := b.store.OpenControlledResource(ctx, ref)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, MaxControlledResourceBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > MaxControlledResourceBytes {
		return nil, ErrForbidden
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != bound.ContentSHA256 {
		return nil, ErrForbidden
	}
	// Re-resolve the server mapping too: a reference can be revoked or rebound
	// during I/O, independently from the parent document grant.
	fresh, err := b.store.LoadControlledResource(ctx, id)
	if err != nil {
		return nil, err
	}
	if fresh == nil || *fresh != bound {
		return nil, ErrForbidden
	}
	if err := g.Revalidate(ctx); err != nil {
		return nil, err
	}
	return body, nil
}
