package core

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"
)

// Explicit controlled input only. No credentials, titles, values, or source
// tokens are printed. This test never creates a Knowledge or changes Feishu ACL.
func TestP4LiveApplicationTableRead(t *testing.T) {
	path := os.Getenv("FEISHU_P4_LIVE_CONFIG")
	if path == "" {
		t.Skip("controlled live application config not supplied")
	}
	var cfg struct {
		Token     string             `json:"token"`
		Resources []PersonalResource `json:"resources"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("live configuration unavailable")
	}
	if json.Unmarshal(data, &cfg) != nil || cfg.Token == "" || len(cfg.Resources) == 0 {
		t.Fatal("invalid live configuration")
	}
	for _, r := range cfg.Resources {
		t.Run(r.Type, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			ctx = WithApplicationIdentity(ctx, func(context.Context) (string, error) { return cfg.Token, nil })
			c := NewClient(&Config{AppID: "p4-controlled-live"})
			s, err := c.CollectApplicationTableSnapshot(ctx, r)
			if errors.Is(err, ErrApplicationTableMedia) {
				t.Log("RESULT=REJECTED_UNSUPPORTED_MEDIA; no product or index acceptance")
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			fingerprint, err := s.Fingerprint()
			if err != nil {
				t.Fatal(err)
			}
			rows, records, fields := 0, 0, 0
			for _, table := range s.Tables {
				rows += len(table.Cells)
				records += len(table.Records)
				fields += len(table.Fields)
			}
			text, err := s.IndexText()
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("RESULT=SOURCE_READ_ONLY tables=%d rows=%d records=%d fields=%d text_bytes=%d fingerprint=%s; product/index/RAG NOT_EXECUTED", len(s.Tables), rows, records, fields, len(text), fingerprint)
		})
	}
}

func TestP4LiveApplicationSheetArtifact(t *testing.T) {
	path := os.Getenv("FEISHU_P4_LIVE_CONFIG")
	if path == "" {
		t.Skip("controlled live application config not supplied")
	}
	var cfg struct {
		Token     string             `json:"token"`
		Resources []PersonalResource `json:"resources"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal("live configuration unavailable")
	}
	if json.Unmarshal(data, &cfg) != nil || cfg.Token == "" {
		t.Fatal("invalid live configuration")
	}
	for _, r := range cfg.Resources {
		if r.Type != "sheet" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()
		ctx = WithApplicationTables(WithApplicationIdentity(ctx, func(context.Context) (string, error) { return cfg.Token, nil }))
		c := NewClient(&Config{AppID: "p4-controlled-live-artifact"})
		content, name, err := c.DownloadApplicationResource(ctx, r)
		if err != nil {
			t.Fatal(err)
		}
		if len(content) == 0 || len(name) < 3 || name[len(name)-3:] != ".md" {
			t.Fatal("invalid formal table artifact")
		}
		t.Logf("RESULT=DOWNLOADED_STRUCTURED_SHEET bytes=%d sha256=%s; Knowledge/index/RAG NOT_EXECUTED", len(content), fmt.Sprintf("%x", sha256.Sum256(content)))
		return
	}
	t.Fatal("no controlled Sheet fixture")
}
