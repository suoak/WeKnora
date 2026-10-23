package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/Tencent/WeKnora/internal/models"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestGatewayBatchLimitP0(t *testing.T) {
	allowLoopback(t)
	for _, n := range []int{1, 20, 21, 40, 41, 100} {
		for _, bounded := range []bool{false, true} {
			t.Run(fmt.Sprintf("n%d/staging%t", n, bounded), func(t *testing.T) {
				var sizes []int
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					var body struct {
						Input []string `json:"input"`
					}
					require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
					sizes = append(sizes, len(body.Input))
					if len(body.Input) > 20 {
						http.Error(w, "batch exceeds 20", 400)
						return
					}
					// Reverse the wire result to verify protocol index ordering too.
					data := []any{}
					for i := len(body.Input) - 1; i >= 0; i-- {
						value, _ := strconv.Atoi(body.Input[i])
						data = append(data, map[string]any{"index": i, "embedding": []float32{float32(value)}})
					}
					require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"data": data}))
				}))
				defer srv.Close()
				config := Config{
					Source:    types.ModelSourceRemote,
					Provider:  "generic",
					BaseURL:   srv.URL,
					ModelName: "text-embedding-v4",
				}
				if !bounded {
					config.ExtraConfig = map[string]string{models.ExtraEmbeddingMaxBatchSize: "20"}
				}
				model, err := newEmbedder(config, nil, nil)
				require.NoError(t, err)
				texts := make([]string, n)
				for i := range texts {
					texts[i] = strconv.Itoa(i)
				}
				var vectors [][]float32
				if bounded {
					vectors, err = BatchEmbedBounded(context.Background(), model, texts, 20)
				} else {
					vectors, err = model.BatchEmbed(context.Background(), texts)
				}
				require.NoError(t, err)
				require.Len(t, vectors, n)
				require.Len(t, sizes, (n+19)/20)
				for i := range vectors {
					require.Equal(t, []float32{float32(i)}, vectors[i])
				}
			})
		}
	}
}

func TestGatewayMiddleBatchFailureReturnsNoCandidateP0(t *testing.T) {
	allowLoopback(t)
	for _, bounded := range []bool{false, true} {
		t.Run(fmt.Sprint(bounded), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if calls == 2 {
					http.Error(w, "injected middle-batch failure", 400)
					return
				}
				var body struct {
					Input []string `json:"input"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				data := []any{}
				for i := range body.Input {
					data = append(data, map[string]any{"index": i, "embedding": []float32{1}})
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
			}))
			defer srv.Close()
			config := Config{
				Source:    types.ModelSourceRemote,
				Provider:  "generic",
				BaseURL:   srv.URL,
				ModelName: "synthetic",
			}
			if !bounded {
				config.ExtraConfig = map[string]string{models.ExtraEmbeddingMaxBatchSize: "20"}
			}
			model, err := newEmbedder(config, nil, nil)
			require.NoError(t, err)
			var got [][]float32
			if bounded {
				got, err = BatchEmbedBounded(context.Background(), model, make([]string, 41), 20)
			} else {
				got, err = model.BatchEmbed(context.Background(), make([]string, 41))
			}
			require.Error(t, err)
			require.Nil(t, got)
			require.Equal(t, 2, calls)
		})
	}
}
