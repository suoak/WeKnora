package service

import (
	"context"
	"errors"
	"math"
	"strconv"
	"testing"

	"github.com/Tencent/WeKnora/internal/application/access"
	"github.com/Tencent/WeKnora/internal/application/repository"
	"github.com/Tencent/WeKnora/internal/models/embedding"
	fixture "github.com/Tencent/WeKnora/internal/testutil/documentaccess"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type portalV1Embedder struct {
	embedding.Embedder
	vector []float32
	err    error
}

func (e portalV1Embedder) Embed(context.Context, string) ([]float32, error) { return e.vector, e.err }

type portalV1Models struct {
	interfaces.ModelService
	model  *types.Model
	vector []float32
	err    error
	called int
	t      *testing.T
}

func (m *portalV1Models) GetModelByID(ctx context.Context, id string) (*types.Model, error) {
	tenant, ok := types.TenantIDFromContext(ctx)
	require.True(m.t, ok)
	require.Equal(m.t, uint64(99), tenant)
	require.Equal(m.t, "synthetic", id)
	return m.model, m.err
}
func (m *portalV1Models) GetEmbeddingModelForTenant(_ context.Context, id string, tenant uint64) (embedding.Embedder, error) {
	require.Equal(m.t, "synthetic", id)
	require.Equal(m.t, uint64(99), tenant)
	m.called++
	return portalV1Embedder{vector: m.vector}, nil
}
func TestPortalV1ModelsNoFallback(t *testing.T) {
	f := fixture.New(t)
	f.PreparePublished(t)
	for _, dimension := range []int{798, 1024, 3584} {
		t.Run(strconv.Itoa(dimension), func(t *testing.T) {
			m := &portalV1Models{t: t, vector: make([]float32, dimension), model: &types.Model{ID: "synthetic", TenantID: 99, Type: types.ModelTypeEmbedding, Status: types.ModelStatusActive, Parameters: types.ModelParameters{EmbeddingParameters: types.EmbeddingParameters{Dimension: dimension}}}}
			s := publishedServiceM7(t, f)
			BindPortalMCPAdapters(s, repository.NewKnowledgeBaseRepository(f.DB), nil, m, nil, nil)
			d := access.PortalKBDescriptor{ID: "public-kb", TenantID: 99, EmbeddingModelID: "synthetic"}
			v, err := s.Embed(fixture.Key("A", 1, 1, true), d, "query")
			require.NoError(t, err)
			require.Len(t, v, dimension)
			m.vector = make([]float32, dimension-1)
			_, err = s.Embed(context.Background(), d, "query")
			require.Error(t, err)
			m.vector = make([]float32, dimension)
			m.vector[0] = float32(math.Inf(1))
			_, err = s.Embed(context.Background(), d, "query")
			require.Error(t, err)
			before := m.called
			m.model.TenantID = 1
			_, err = s.Embed(context.Background(), d, "query")
			require.Error(t, err)
			require.Equal(t, before, m.called)
			m.model.TenantID = 99
			m.model.Status = "inactive"
			_, err = s.Embed(context.Background(), d, "query")
			require.Error(t, err)
			require.Equal(t, before, m.called)
			m.model.Status = types.ModelStatusActive
			m.err = errors.New("model unavailable")
			_, err = s.Embed(context.Background(), d, "query")
			require.Error(t, err)
			require.Equal(t, before, m.called)
		})
	}
}
