package feishusynccontrol

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Keep the first stop's bounded, argument-free probe evidence in Redis audit.
// Container replacement must not erase the only evidence for a retained trip.
func (c *Controller) failureTiming() string {
	t := c.timings
	t.Finished, t.Failed = time.Now().UTC(), true
	t.Stages = append([]StageTiming(nil), t.Stages...)
	if n := len(t.Stages); n > 0 {
		t.Stages[n-1].Finished = t.Finished
	}
	if c.Transport != nil {
		t.Commands, t.IO = c.Transport.snapshot()
	}
	if len(t.Commands) > 8 {
		t.Commands = t.Commands[len(t.Commands)-8:]
	}
	if len(t.IO) > 16 {
		t.IO = t.IO[len(t.IO)-16:]
	}
	encoded, _ := json.Marshal(t)
	return string(encoded)
}

// StepTiming contains no source IDs, command arguments, credentials or bodies.
// Observer must be nonblocking; deployment wiring uses a bounded channel.
type StepTiming struct {
	Requested   time.Time       `json:"requested"`
	LockWaitNS  int64           `json:"lock_wait_ns"`
	ID          string          `json:"id"`
	WorkerID    string          `json:"worker_id"`
	Incarnation string          `json:"incarnation"`
	Started     time.Time       `json:"started"`
	Finished    time.Time       `json:"finished"`
	Stages      []StageTiming   `json:"stages"`
	Commands    []CommandTiming `json:"commands"`
	IO          []IOTiming      `json:"io"`
	Failed      bool            `json:"failed"`
}

// StageTiming separates control phases and their remaining deadline budgets.
type StageTiming struct {
	Stage       string    `json:"stage"`
	Started     time.Time `json:"started"`
	Finished    time.Time `json:"finished"`
	RemainingMS int64     `json:"remaining_ms"`
}

func (c *Controller) stage(ctx context.Context, name string) {
	now := time.Now().UTC()
	if n := len(c.timings.Stages); n > 0 {
		c.timings.Stages[n-1].Finished = now
	}
	remaining := int64(-1)
	if deadline, ok := ctx.Deadline(); ok {
		remaining = time.Until(deadline).Milliseconds()
	}
	c.probeStage = name
	c.timings.Stages = append(
		c.timings.Stages,
		StageTiming{Stage: name, Started: now, RemainingMS: remaining},
	)
}

func (c *Controller) beginTiming(ctx context.Context) {
	c.timings = StepTiming{
		ID: uuid.NewString(), WorkerID: c.Config.WorkerID,
		Incarnation: c.incarnation, Started: time.Now().UTC(),
	}
	if c.Transport != nil {
		c.Transport.begin(ctx)
	}
	c.stage(ctx, "registry")
}

func (c *Controller) finishTiming(err error) {
	now := time.Now().UTC()
	c.timings.Finished, c.timings.Failed = now, err != nil
	if n := len(c.timings.Stages); n > 0 {
		c.timings.Stages[n-1].Finished = now
	}
	if c.Transport != nil {
		c.timings.Commands, c.timings.IO = c.Transport.finish()
	}
	if c.Observe != nil {
		c.Observe(c.timings)
	}
}
