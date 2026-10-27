package feishusynccontrol

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestAutomaticExpiredAuditIsNotMisclassifiedAsMembershipProbe(t *testing.T) {
	f := newControlFixture(t)
	c := f.automaticController(t, "app:1")
	consumerCalls := 0
	c.Consumers = func(context.Context) ([]string, error) {
		consumerCalls++
		return []string{"app:1"}, nil
	}
	if err := c.Step(context.Background()); err != nil {
		t.Fatal(err)
	}
	consumerCalls, c.lastAlertAck = 0, time.Time{}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	wantDeadline, _ := ctx.Deadline()
	var stopped Event
	c.Audit = func(event Event) error {
		if event.Kind == "feishu_safety_heartbeat" {
			deadline, ok := event.Context().Deadline()
			if !ok || deadline.After(wantDeadline) {
				t.Fatal("audit exceeded the controller step budget")
			}
			<-event.Context().Done()
			return nil // A sink acknowledging late must still close the step.
		}
		stopped = event
		if event.Context().Err() != nil {
			t.Fatal("expired probe canceled independent stop audit")
		}
		return nil
	}
	if c.Step(ctx) == nil || consumerCalls != 0 {
		t.Fatal("expired heartbeat proceeded to consumer or permit probe")
	}
	if stopped.Reason != "audit_unavailable" || stopped.ProbeStage != "audit" {
		t.Fatalf("misclassified timeout: reason=%s stage=%s", stopped.Reason, stopped.ProbeStage)
	}
	if f.client.Get(context.Background(), c.prefix+"trip").Val() != "audit_unavailable" ||
		f.client.Get(context.Background(), "asynq:{sync}:paused").Val() != "feishu_safety_stop" ||
		f.guard.Check(context.Background()) == nil {
		t.Fatal("audit failure did not retain stop latch, paused queue and closed permit")
	}
}

func TestAlertSinkHonorsControlCancellation(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	sink, err := NewAlertSink(server.URL, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sink(Event{Kind: "feishu_safety_heartbeat", ctx: ctx}) == nil || requests.Load() != 0 {
		t.Fatal("canceled control audit sent an unbounded request")
	}
}
