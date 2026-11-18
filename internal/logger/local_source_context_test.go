package logger

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestCloneContextRetainsPlatformContentPolicyAndReadScope(t *testing.T) {
	for _, clone := range []func(context.Context) context.Context{CloneContext, CloneContextWithoutTrace} {
		ctx := types.WithLocalSourceContentRead(types.WithLegacyReadScope(context.Background()))
		detached := clone(ctx)
		require.True(t, types.LocalSourceContentRead(detached), "async QA uses the same platform reading policy")
		types.RecordLegacyReadExclusion(detached)
		require.True(t, types.LegacyReadPartial(ctx), "actual reads remain attached to the response scope")
		require.False(t, types.LocalSourceContentRead(clone(context.Background())))
	}
}
