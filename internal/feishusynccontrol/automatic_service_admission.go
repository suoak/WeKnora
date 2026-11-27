package feishusynccontrol

import (
	"errors"
	"strings"

	"github.com/redis/go-redis/v9"
)

// ErrAutomaticCanaryPending defers an already authorized source intent while
// another source qualifies the automatic service. It grants no outbound access
// and does not classify identity, permission, transport or hard-stop failures.
var ErrAutomaticCanaryPending = errors.New("automatic service Canary admission pending")

func automaticSourceAdmissionError(err error) error {
	if err == nil {
		return nil
	}
	var reply redis.Error
	if errors.As(err, &reply) && strings.TrimPrefix(reply.Error(), "ERR ") == "SYSTEM_CANARY_OTHER_SOURCE" {
		return errors.Join(ErrNotReady, ErrAutomaticCanaryPending, err)
	}
	return errors.Join(ErrNotReady, err)
}
