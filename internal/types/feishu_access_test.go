package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFeishuAccessCapabilitiesRequireExplicitEnabledIdentity(t *testing.T) {
	for flags := 0; flags < 8; flags++ {
		p := FeishuAccessCapabilities{flags&1 != 0, flags&2 != 0, flags&4 != 0}
		for _, mode := range []string{"", "unknown", FeishuUserOAuth, FeishuApplicationGrant} {
			got, err := p.ResolveMode(mode)
			allowed := p.RolloutEnabled && (mode == FeishuUserOAuth && p.PersonalEnabled ||
				mode == FeishuApplicationGrant && p.ApplicationEnabled)
			if allowed && (err != nil || got != mode) || !allowed && (err == nil || got != "") {
				t.Fatalf("flags=%d requested=%q got=%q err=%v", flags, mode, got, err)
			}
		}
	}
}

func TestFeishuCredentialRefOwnerAndKindIsolation(t *testing.T) {
	user := FeishuCredentialRef{
		Version: 1, Kind: FeishuUserOAuth, ConnectionID: "connection",
		AppID:         "cli_test",
		OwnerID:       "A",
		OpenID:        "private-open-id",
		GrantEpoch:    "private-epoch",
		ConfigVersion: "private-config",
	}
	if err := user.Validate("A"); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*FeishuCredentialRef){
		func(r *FeishuCredentialRef) { r.Version = 2 },
		func(r *FeishuCredentialRef) { r.Kind = FeishuApplicationGrant },
		func(r *FeishuCredentialRef) { r.OwnerID = "B" },
		func(r *FeishuCredentialRef) { r.OpenID = "" },
		func(r *FeishuCredentialRef) { r.GrantEpoch = "" },
		func(r *FeishuCredentialRef) { r.ConfigVersion = "" },
		func(r *FeishuCredentialRef) { r.ConnectionID = "" },
		func(r *FeishuCredentialRef) { r.AppID = "" },
	} {
		bad := user
		mutate(&bad)
		if bad.Validate("A") == nil {
			t.Fatalf("invalid reference accepted: %+v", bad)
		}
	}
	if user.Validate("B") == nil || user.Validate("") == nil {
		t.Fatal("cross-user reference accepted")
	}
	app := user
	app.Kind, app.OwnerID, app.OpenID, app.GrantEpoch = FeishuApplicationGrant, "", "", ""
	if err := app.Validate("A"); err != nil {
		t.Fatal(err)
	}
	app.GrantEpoch = "other-user-grant"
	if app.Validate("A") == nil {
		t.Fatal("application identity accepted a user grant")
	}
	data, err := json.Marshal(user)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private-open-id", "private-epoch", "private-config"} {
		if strings.Contains(string(data), secret) {
			t.Fatal("private identity metadata serialized")
		}
	}
}
