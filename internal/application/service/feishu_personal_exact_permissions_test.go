package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/datasource/connector/feishu/core"
	"github.com/Tencent/WeKnora/internal/utils"
	"github.com/stretchr/testify/require"
)

func TestExactPersonalPermissionsParallel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	started := make(chan string, 3)
	release := make(chan struct{})
	type result struct {
		permissions core.PersonalPermissions
		err         error
	}
	completed := make(chan result, 1)
	go func() {
		permissions, err := inspectExactPersonalPermissions(ctx,
			func(current context.Context, action string) (bool, error) {
				started <- action
				select {
				case <-release:
					return true, nil
				case <-current.Done():
					return false, current.Err()
				}
			})
		completed <- result{permissions, err}
	}()
	seen := map[string]bool{}
	for range 3 {
		select {
		case action := <-started:
			require.False(t, seen[action], "one request per action")
			seen[action] = true
		case <-ctx.Done():
			t.Fatal("independent checks must all start before any answer is released")
		}
	}
	close(release)
	got := <-completed
	require.NoError(t, got.err)
	require.Equal(t, core.PersonalPermissions{View: true, Export: true, Copy: true}, got.permissions)
}

func TestExactPersonalPermissionsDeniedOrUnknown(t *testing.T) {
	for _, action := range []string{"view", "export", "copy"} {
		t.Run(action, func(t *testing.T) {
			permissions, err := inspectExactPersonalPermissions(context.Background(),
				func(_ context.Context, current string) (bool, error) { return current != action, nil })
			require.NoError(t, err)
			require.False(t, permissions.View && permissions.Export && permissions.Copy)
			unavailable := errors.New("provider unavailable")
			permissions, err = inspectExactPersonalPermissions(context.Background(),
				func(_ context.Context, current string) (bool, error) {
					if current == action {
						return true, unavailable // A boolean alongside an error never grants access.
					}
					return true, nil
				})
			require.ErrorIs(t, err, unavailable)
			require.ErrorIs(t, err, core.ErrPersonalBlocksPolicyUnverified)
			require.Equal(t, core.PersonalPermissions{}, permissions)
		})
	}
}

func TestExactPersonalPermissionsCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var calls atomic.Int32
	permissions, err := inspectExactPersonalPermissions(ctx, func(context.Context, string) (bool, error) {
		calls.Add(1)
		return true, nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Zero(t, calls.Load())
	require.Equal(t, core.PersonalPermissions{}, permissions)
}

func TestExactPersonalPermissionsClientIdentity(t *testing.T) {
	// Only this disposable process's httptest peer is allowed. Production SSRF
	// checks and configuration are unchanged; the first run correctly refused it.
	t.Setenv("SSRF_WHITELIST", "127.0.0.1")
	utils.ResetSSRFWhitelistForTest()
	t.Cleanup(utils.ResetSSRFWhitelistForTest)
	var mu sync.Mutex
	actions := map[string]int{}
	var arrived atomic.Int32
	var releaseOnce sync.Once
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer isolated-personal-user" {
			t.Error("permission checks must retain the same personal identity")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		action := r.URL.Query().Get("action")
		mu.Lock()
		actions[action]++
		mu.Unlock()
		if arrived.Add(1) >= 2 {
			releaseOnce.Do(func() { close(release) })
		}
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]bool{"auth_result": true}})
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ctx = core.WithUserIdentity(ctx, func(context.Context) (string, error) {
		return "isolated-personal-user", nil
	})
	client := core.NewClient(&core.Config{AppID: "isolated-exact-permission-concurrency", BaseURL: server.URL})
	permissions, err := inspectExactPersonalPermissions(ctx,
		func(current context.Context, action string) (bool, error) {
			resource := core.PersonalResource{Type: "docx", Token: "document"}
			return client.PersonalResourcePermission(current, resource, action)
		})
	require.NoError(t, err)
	require.Equal(t, core.PersonalPermissions{View: true, Export: true, Copy: true}, permissions)
	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, map[string]int{"view": 1, "export": 1, "copy": 1}, actions)
}

func TestExactPersonalPermissionsCanceledAfterAnswers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	permissions, err := inspectExactPersonalPermissions(ctx, func(context.Context, string) (bool, error) {
		cancel()
		return true, nil
	})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, core.PersonalPermissions{}, permissions)
}
