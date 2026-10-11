package types

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestB2StreamFailureHasNoPublicCause(t *testing.T) {
	for code := StreamFailureUnknown; code <= StreamFailureRequestRejected; code++ {
		e := NewStreamFailure(code, 429, 100*time.Second)
		if errors.Unwrap(e) != nil {
			t.Fatal("retained original cause")
		}
		if e.RetryAfter() > 30*time.Second {
			t.Fatal("unbounded retry")
		}
		b, err := json.Marshal(StreamResponse{ResponseType: ResponseTypeError, Content: e.PublicContent(), Failure: e})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(b), "failure") || strings.Contains(string(b), "retry") || strings.Contains(string(b), "code") {
			t.Fatal("internal classification serialized")
		}
		if got, _ := json.Marshal(e); string(got) != "{}" {
			t.Fatal("internal error exported")
		}
		for i := 0; i < reflect.TypeOf(*e).NumField(); i++ {
			k := reflect.TypeOf(*e).Field(i).Type.Kind()
			if k == reflect.Interface || k == reflect.String || k == reflect.Map || k == reflect.Slice {
				t.Fatal("unsafe field")
			}
		}
		_ = fmt.Sprintf("%+v %#v", e, e)
	}
	e := NewStreamFailure(StreamFailureCode(255), 999, -time.Second)
	if e.Code() != StreamFailureUnknown || e.Status() != 0 || e.RetryAfter() != 0 {
		t.Fatal("unvalidated values")
	}
	if !errors.Is(NewStreamFailure(StreamFailureCanceled, 0, 0), context.Canceled) || !errors.Is(NewStreamFailure(StreamFailureDeadline, 0, 0), context.DeadlineExceeded) {
		t.Fatal("lost context classification")
	}
	var event StreamResponse
	if err := json.Unmarshal([]byte(`{"failure":{"code":3},"data":{"failure":"rate_limited"}}`), &event); err != nil || event.Failure != nil {
		t.Fatal("public injection")
	}
}
