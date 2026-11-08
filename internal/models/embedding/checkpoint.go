package embedding

import "context"

type batchCheckpointKey struct{}

// WithBatchCheckpoint persists each validated physical response before a later
// vendor/gateway sub-batch can fail. Only trusted staging code installs it.
// The callback must enforce its own SQL ownership and source permission fences.
func WithBatchCheckpoint(ctx context.Context, save func(context.Context, []string, [][]float32) error) context.Context {
	return context.WithValue(ctx, batchCheckpointKey{}, save)
}

func checkpointBatch(ctx context.Context, inputs []string, vectors [][]float32) error {
	if save, ok := ctx.Value(batchCheckpointKey{}).(func(context.Context, []string, [][]float32) error); ok {
		return save(ctx, inputs, vectors)
	}
	return nil
}
