package embedding

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/models/api"
	"github.com/Tencent/WeKnora/internal/models/api/openaiembeddings"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func reservationEmbedder(t *testing.T, failure int, timeout time.Duration) (*protocolEmbedder, *miniredis.Miniredis, *redis.Client, *feishusafety.Guard, *atomic.Int32) {
	t.Helper()
	allowLoopback(t)
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { client.Close() })
	var calls atomic.Int32
	clock := time.Now()
	server.SetTime(clock)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := int(calls.Add(1))
		var payload struct {
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		cost := 0
		for _, s := range payload.Input {
			cost += len(s) + 128
		}
		if cost > 30000 || len(payload.Input) > 20 {
			t.Errorf("unsafe wire cost %d/%d", cost, len(payload.Input))
		}
		if failure == n {
			http.Error(w, "injected", 500)
			return
		}
		if failure == -1 && n == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "busy", 429)
			return
		}
		var data []any
		for i := len(payload.Input) - 1; i >= 0; i-- {
			prefix := strings.SplitN(payload.Input[i], ":", 2)[0]
			v, _ := strconv.Atoi(prefix)
			data = append(data, map[string]any{"index": i, "embedding": []float32{float32(v)}})
		}
		clock = clock.Add(61 * time.Second)
		server.SetTime(clock)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	policy := feishusafety.DefaultConfig()
	policy.Namespace = "embedding-reservation"
	policy.EndpointHost = strings.TrimPrefix(srv.URL, "http://")
	policy.RPS = 1000
	policy.RPM = 1000
	g, err := feishusafety.New(client, policy)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.Set(context.Background(), g.PermitKey(), g.Fingerprint(), time.Hour).Err(); err != nil {
		t.Fatal(err)
	}
	feishusafety.InstallDefault(g)
	t.Cleanup(func() { feishusafety.InstallDefault(nil) })
	inner := openaiembeddings.New(openaiembeddings.Config{Endpoint: api.Endpoint{BaseURL: srv.URL, Model: "m", Client: &http.Client{Timeout: timeout, Transport: feishusafety.Transport{Base: http.DefaultTransport, Embedding: true}}}})
	return &protocolEmbedder{inner: inner, settings: api.EmbeddingsSettings{MaxBatchSize: 20}, modelName: "m"}, server, client, g, &calls
}

func TestReservedChineseBatchesP0(t *testing.T) {
	for _, n := range []int{20, 21, 100} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			model, _, _, _, calls := reservationEmbedder(t, 0, time.Second)
			inputs := make([]string, n)
			for i := range inputs {
				inputs[i] = fmt.Sprintf("%d:%s", i, strings.Repeat("知", 512))
			}
			vectors, err := model.BatchEmbed(feishusafety.WithScope(context.Background(), "source"), inputs)
			if err != nil {
				t.Fatal(err)
			}
			if len(vectors) != n {
				t.Fatal("missing results")
			}
			for i, v := range vectors {
				if v[0] != float32(i) {
					t.Fatal("order changed", i, v)
				}
			}
			if calls.Load() < 2 {
				t.Fatal("large Chinese batch not split")
			}
		})
	}
}

func TestReservedMiddleBatchFailureP0(t *testing.T) {
	model, _, _, _, _ := reservationEmbedder(t, 2, time.Second)
	inputs := make([]string, 20)
	for i := range inputs {
		inputs[i] = strings.Repeat("知", 512)
	}
	vectors, err := model.BatchEmbed(feishusafety.WithScope(context.Background(), "source"), inputs)
	if err == nil || vectors != nil {
		t.Fatal("partial embeddings published", err)
	}
}

func TestReservationWaitOutsideHTTPTimeoutP0(t *testing.T) {
	model, server, _, _, _ := reservationEmbedder(t, 0, 50*time.Millisecond)
	ctx := feishusafety.WithScope(context.Background(), "source")
	_, release, err := feishusafety.ReserveEmbedding(ctx, []string{strings.Repeat("a", 29000)})
	if err != nil {
		t.Fatal(err)
	}
	release()
	done := make(chan struct{})
	go func() {
		time.Sleep(150 * time.Millisecond)
		server.SetTime(time.Now().Add(61 * time.Second))
		close(done)
	}()
	defer func() { <-done }()
	vectors, err := model.BatchEmbed(ctx, []string{strings.Repeat("b", 2000)})
	if err != nil || len(vectors) != 1 {
		t.Fatal("local budget wait consumed HTTP timeout", err)
	}
}

func TestReserved429FreshWireBudgetP0(t *testing.T) {
	model, _, client, g, calls := reservationEmbedder(t, -1, 100*time.Millisecond)
	ctx := feishusafety.WithScope(context.Background(), "source")
	vectors, err := model.BatchEmbed(ctx, []string{"0:abc"})
	if err != nil || len(vectors) != 1 || calls.Load() != 2 {
		t.Fatal("bounded retry failed", err, calls.Load())
	}
	stats, err := g.Snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	_ = client
	if stats["inflight"].(int64) != 0 {
		t.Fatal("leaked retry slot", stats)
	}
}
