package service

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
)

type sourceIDJITStore interface {
	ResolveSourceIDEnterpriseOrJITIdentity(
		context.Context,
		string,
		string,
		string,
		string,
		string,
		string,
		time.Duration,
		string,
		[]string,
		func(context.Context, interfaces.UserRepository) (*types.User, error),
	) (*types.User, error)
}

type (
	sourceIDJITCreatedKey      struct{}
	sourceIDJITExternalOnlyKey struct{}
)

// Keep driver error details out of Register's generic error logging, while
// reusing the existing tenantless registration and password policy behavior.
type sourceIDJITUsers struct{ interfaces.UserRepository }

func (r sourceIDJITUsers) CreateUser(ctx context.Context, u *types.User) error {
	if r.UserRepository.CreateUser(ctx, u) != nil {
		return errors.New("sourceid jit registration failed")
	}
	return nil
}

func (r sourceIDJITUsers) UpdateUser(ctx context.Context, u *types.User) error {
	if r.UserRepository.UpdateUser(ctx, u) != nil {
		return errors.New("sourceid jit registration failed")
	}
	return nil
}

func (s *userService) sourceIDJITProvisioner(
	email string,
) func(context.Context, interfaces.UserRepository) (*types.User, error) {
	return func(ctx context.Context, repository interfaces.UserRepository) (*types.User, error) {
		username, err := sourceIDJITUsername(ctx, repository, email)
		if err != nil {
			return nil, err
		}
		password := make([]byte, 32)
		if _, err := rand.Read(password); err != nil {
			return nil, errors.New("sourceid jit random unavailable")
		}
		scoped := *s
		scoped.userRepo = sourceIDJITUsers{repository}
		registrationCtx := context.WithValue(ctx, sourceIDJITExternalOnlyKey{}, true)
		user, err := scoped.Register(
			registrationCtx,
			&types.RegisterRequest{
				Username:           username,
				Email:              email,
				Password:           base64.RawURLEncoding.EncodeToString(password),
				TenantProvisioning: types.TenantProvisioningTenantless,
			},
		)
		if err != nil {
			return nil, errors.New("sourceid jit registration failed")
		}
		user.PasswordHash = "!sourceid-jit-no-local-password"
		only := true
		user.Preferences.OidcOnlyLogin = &only
		if err := scoped.userRepo.UpdateUser(ctx, user); err != nil {
			return nil, err
		}
		return user, nil
	}
}

// Called using the transaction-scoped repository under the existing PostgreSQL
// JIT registration lock. A name collision never grants ownership of that user.
// Existing identities bypass provisioning and keep their stable ID and username.
func sourceIDJITUsername(ctx context.Context, repository interfaces.UserRepository, email string) (string, error) {
	local, domain, ok := strings.Cut(strings.TrimSpace(email), "@")
	if !ok || domain == "" || strings.Contains(domain, "@") {
		return "", errors.New("sourceid jit username unavailable")
	}
	base := sanitizeUsernameCandidate(local)
	if base == "" {
		return "", errors.New("sourceid jit username unavailable")
	}
	for i := 1; i <= 100; i++ {
		candidate := base
		if i > 1 {
			suffix := fmt.Sprintf("-%d", i)
			prefix := base
			if len(prefix)+len(suffix) > 50 {
				prefix = strings.TrimRight(prefix[:50-len(suffix)], "-._")
			}
			candidate = prefix + suffix
		}
		existing, err := repository.GetUserByUsername(ctx, candidate)
		if err != nil && !isUserLookupNotFound(err) {
			return "", errors.New("sourceid jit username unavailable")
		}
		if existing == nil {
			return candidate, nil
		}
	}
	return "", errors.New("sourceid jit username unavailable")
}
