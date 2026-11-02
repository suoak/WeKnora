package feishusynccontrol

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/feishusafety"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type steadyCanceledBody struct{ ctx context.Context }

func (b steadyCanceledBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}
func (steadyCanceledBody) Close() error { return nil }

// Cancellation is tested during both headers and response reads. A source-local
// denial must still discard that response while independent sources stay closed
// only when the shared control state actually fails.
func TestSteadyInflightSourceRevocationDoesNotTripOtherSources(t *testing.T) {
	for _, headers := range []bool{false, true} {
		for _, deny := range []string{"authority", "claim"} {
			for _, kind := range []string{"source", "embedding", "background_model"} {
				t.Run(kind+"_"+deny+map[bool]string{false: "_headers", true: "_body"}[headers], func(t *testing.T) {
					f := newSteadyFixture(t)
					f.activate(t)
					ctx := context.Background()
					id := f.ids[0]
					var revoked atomic.Bool
					authority := f.m.steadyAuthority
					facts := f.m.facts
					f.m.steadyAuthority = func(
						c context.Context, source, actor string,
					) (SourceManagementAuthority, error) {
						if source == id && deny == "authority" && revoked.Load() {
							return SourceManagementAuthority{}, ErrRecoveryRejected
						}
						return authority(c, source, actor)
					}
					f.m.facts = func(c context.Context, source string) (RecoveryFacts, error) {
						v, err := facts(c, source)
						if source == id && deny == "claim" && revoked.Load() {
							v.Eligible, v.RetryEligible = false, false
							v.sourceStateKnown = true
						}
						return v, err
					}
					work, _, err := BindApprovedSource(ctx, id, f.facts[id].ConfigSHA256)
					require.NoError(t, err)
					work = feishusafety.WithRecoveryExecution(work, f.facts[id].ConfigSHA256, uuid.NewString(), 1)
					identity, _ := feishusafety.RecoveryOutboundExecution(work)
					work = types.WithSyncExecution(work, types.SyncExecution{
						DataSourceID: id, TenantID: f.authority[id].TenantID,
						SyncLogID: identity.Run, Generation: 1,
					})
					work, cancel := context.WithTimeout(feishusafety.WithScope(work, id), 3*time.Second)
					defer cancel()
					started := make(chan struct{})
					base := continuityRoundTripper(func(req *http.Request) (*http.Response, error) {
						close(started)
						if !headers {
							<-req.Context().Done()
							return nil, req.Context().Err()
						}
						return &http.Response{
							StatusCode: http.StatusOK, Header: http.Header{}, Body: steadyCanceledBody{req.Context()},
						}, nil
					})
					transport := feishusafety.Transport{Base: base}
					transport.Embedding = kind == "embedding"
					transport.BackgroundModel = kind == "background_model"
					req, err := http.NewRequestWithContext(work, http.MethodPost,
						"https://gateway.invalid/document", strings.NewReader(`{"input":["one"]}`))
					require.NoError(t, err)
					done := make(chan error, 1)
					go func() {
						resp, err := transport.RoundTrip(req)
						if err == nil {
							_, err = io.ReadAll(resp.Body)
							_ = resp.Body.Close()
						}
						done <- err
					}()
					select {
					case <-started:
					case <-time.After(time.Second):
						t.Fatal("request did not start")
					}
					revoked.Store(true)
					select {
					case err := <-done:
						require.ErrorIs(t, err, feishusafety.ErrClosed,
							"withdrawn source response must not be accepted")
					case <-time.After(2 * time.Second):
						t.Fatal("withdrawn source request did not drain")
					}
					require.Zero(t, f.r.Exists(ctx, f.c.prefix+"trip").Val(),
						"one withdrawn source must not latch a shared stop")
					_, approved, err := BindApprovedSource(ctx, f.ids[1], f.facts[f.ids[1]].ConfigSHA256)
					require.NoError(t, err)
					require.True(t, approved)
				})
			}
		}
	}
}

func TestSteadyInflightUnknownAuthorityFailureStillTripsSharedGate(t *testing.T) {
	for _, headers := range []bool{false, true} {
		for _, fault := range []string{"authority_timeout", "control_unconfirmed"} {
			t.Run(fault+map[bool]string{false: "_headers", true: "_body"}[headers], func(t *testing.T) {
				f := newSteadyFixture(t)
				f.activate(t)
				ctx := context.Background()
				id := f.ids[0]
				var failed atomic.Bool
				load := f.m.steadyAuthority
				f.m.steadyAuthority = func(c context.Context, source, actor string) (SourceManagementAuthority, error) {
					if fault == "authority_timeout" && failed.Load() {
						return SourceManagementAuthority{}, context.DeadlineExceeded
					}
					return load(c, source, actor)
				}
				facts := f.m.facts
				f.m.facts = func(c context.Context, source string) (RecoveryFacts, error) {
					v, err := facts(c, source)
					if fault == "control_unconfirmed" && failed.Load() {
						v.Eligible, v.sourceStateKnown = false, false
					}
					return v, err
				}
				work, _, err := BindApprovedSource(ctx, id, f.facts[id].ConfigSHA256)
				require.NoError(t, err)
				run := uuid.NewString()
				work = feishusafety.WithRecoveryExecution(work, f.facts[id].ConfigSHA256, run, 1)
				work = types.WithSyncExecution(work, types.SyncExecution{
					DataSourceID: id, TenantID: f.authority[id].TenantID, SyncLogID: run, Generation: 1,
				})
				work, cancel := context.WithTimeout(feishusafety.WithScope(work, id), 3*time.Second)
				defer cancel()
				started := make(chan struct{})
				base := continuityRoundTripper(func(req *http.Request) (*http.Response, error) {
					close(started)
					if !headers {
						<-req.Context().Done()
						return nil, req.Context().Err()
					}
					return &http.Response{
						StatusCode: http.StatusOK, Header: http.Header{}, Body: steadyCanceledBody{req.Context()},
					}, nil
				})
				transport := feishusafety.Transport{Base: base}
				req, err := http.NewRequestWithContext(work, http.MethodGet, "https://source.invalid/document", nil)
				require.NoError(t, err)
				done := make(chan error, 1)
				go func() {
					resp, err := transport.RoundTrip(req)
					if err == nil {
						_, err = io.ReadAll(resp.Body)
						_ = resp.Body.Close()
					}
					done <- err
				}()
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("request did not start")
				}
				failed.Store(true)
				select {
				case err := <-done:
					require.ErrorIs(t, err, feishusafety.ErrClosed)
				case <-time.After(2 * time.Second):
					t.Fatal("unknown authority failure did not stop the request")
				}
				require.EqualValues(t, 1, f.r.Exists(ctx, f.c.prefix+"trip").Val())
				require.Zero(t, f.r.Exists(ctx, f.c.Guard.PermitKey()).Val())
			})
		}
	}
}

func TestSteadyValidationReadsOneLiveFactsSnapshotPerCheck(t *testing.T) {
	f := newSteadyFixture(t)
	f.activate(t)
	load := f.m.facts
	calls := 0
	f.m.facts = func(ctx context.Context, id string) (RecoveryFacts, error) {
		calls++
		return load(ctx, id)
	}
	require.NoError(t, f.m.checkApprovedSource(context.Background(), f.ids[0], f.facts[f.ids[0]].ConfigSHA256))
	require.Equal(t, 1, calls, "do not repeat the same multi-query authoritative load within one validation")
}

func TestSteadyRuntimeFactsProofDoesNotChangeSignedResetWireContract(t *testing.T) {
	f := newRecoveryFixture(t)
	ctx := context.Background()
	f.facts.sourceStateKnown = true
	plan, err := f.m.Plan(ctx, f.facts.SourceID, "operator")
	require.NoError(t, err)
	before, err := RecoveryDigest(plan)
	require.NoError(t, err)
	encoded, err := json.Marshal(plan)
	require.NoError(t, err)
	var decoded RecoveryPlan
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.False(t, decoded.Facts.sourceStateKnown)
	after, err := RecoveryDigest(decoded)
	require.NoError(t, err)
	require.Equal(t, before, after)
	changed := decoded.Facts
	changed.ConfigSHA256 = strings.Repeat("e", 64)
	require.False(t, sameRecoveryFacts(plan.Facts, changed), "a signed field change must still fail")
	f.p = decoded
	require.NoError(t, f.m.Reset(ctx, decoded, f.approve(t, "reset"), "operator"))
}
