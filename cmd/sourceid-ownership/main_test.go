package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

type syntheticStore struct {
	calls   int
	applied bool
}

func (s *syntheticStore) ResolveSourceIDOwnershipAccount(_ context.Context, _ string, employee string, _ string) (string, error) {
	return "synthetic-" + employee, nil
}

func (s *syntheticStore) SyncSourceIDAccountOwnership(_ context.Context, row types.SourceIDAccountOwnership, _ string, apply bool) (bool, error) {
	s.calls++
	s.applied = apply
	if row.EmployeeNo == "synthetic-conflict" {
		return false, errors.New("private input must not escape")
	}
	return true, nil
}
func TestSourceIDOwnershipStreamingAndRedaction(t *testing.T) {
	for _, n := range []int{1000, 10000} {
		var input bytes.Buffer
		for i := 0; i < n; i++ {
			b, _ := json.Marshal(protectedRow{Issuer: "synthetic-issuer", UserID: fmt.Sprint(i), EmployeeNo: fmt.Sprintf("synthetic-private-employee-%d", i), Email: fmt.Sprintf("synthetic-private-%d@example.test", i)})
			input.Write(b)
			input.WriteByte('\n')
		}
		var out bytes.Buffer
		store := &syntheticStore{}
		require.NoError(t, process(context.Background(), &input, &out, store, "synthetic-operator", false))
		require.Equal(t, n, store.calls)
		require.False(t, store.applied)
		require.NotContains(t, out.String(), "employee")
		require.NotContains(t, out.String(), "@")
		var result summary
		require.NoError(t, json.Unmarshal(out.Bytes(), &result))
		require.Equal(t, n, result.Changed)
	}
	var out bytes.Buffer
	store := &syntheticStore{}
	require.Error(t, process(context.Background(), strings.NewReader("{\"employee_no\":\"synthetic-ok\"}\n{\"employee_no\":\"synthetic-conflict\"}\n{\"unreviewed_field\":true}\n"), &out, store, "synthetic-operator", true))
	var result summary
	require.NoError(t, json.Unmarshal(out.Bytes(), &result))
	require.Equal(t, 1, result.Changed)
	require.Equal(t, 2, result.Rejected)
	require.NotContains(t, out.String(), "private")
}

func TestSourceIDOwnershipConflictingBatchRejectedBeforeWrite(t *testing.T) {
	for _, apply := range []bool{false, true} {
		var input, out bytes.Buffer
		a := protectedRow{Issuer: "synthetic-issuer", UserID: "synthetic-user", EmployeeNo: "synthetic-employee", Email: "synthetic@example.test"}
		b, _ := json.Marshal(a)
		input.Write(b)
		input.WriteByte('\n')
		input.Write(b)
		input.WriteByte('\n')
		a.EmployeeNo = "synthetic-reallocated"
		b, _ = json.Marshal(a)
		input.Write(b)
		input.WriteByte('\n')
		store := &syntheticStore{}
		require.Error(t, process(context.Background(), &input, &out, store, "synthetic-operator", apply))
		require.Equal(t, 1, store.calls)
		var result summary
		require.NoError(t, json.Unmarshal(out.Bytes(), &result))
		require.Equal(t, 1, result.Unchanged)
		require.Equal(t, 1, result.Rejected)
	}
}
