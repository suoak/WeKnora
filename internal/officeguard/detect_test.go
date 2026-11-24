package officeguard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	werrors "github.com/Tencent/WeKnora/internal/errors"
	"os"
	"testing"
	"time"
)

func TestFixtures(t *testing.T) {
	var cases []struct {
		Name  string
		State State
	}
	data, err := os.ReadFile("testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, tc := range cases {
		t.Run(tc.Name, func(t *testing.T) {
			p, err := os.ReadFile("testdata/" + tc.Name)
			if err != nil {
				t.Fatal(err)
			}
			got := Detect(context.Background(), bytes.NewReader(p), int64(len(p)), tc.Name)
			if got != tc.State {
				t.Fatalf("got %s want %s", got, tc.State)
			}
			err = Validate(context.Background(), bytes.NewReader(p), int64(len(p)), tc.Name)
			var app *werrors.AppError
			if tc.State == Encrypted {
				if !errors.As(err, &app) || app.Code != 2400 || app.HTTPCode != 400 {
					t.Fatalf("expected typed rejection, got %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestVendorFixtures(t *testing.T) {
	var provenance struct {
		Fixtures []struct {
			Name  string
			State State
		}
	}
	p, e := os.ReadFile("testdata/vendor/provenance.json")
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(p, &provenance); e != nil {
		t.Fatal(e)
	}
	for _, tc := range provenance.Fixtures {
		t.Run(tc.Name, func(t *testing.T) {
			p, e := os.ReadFile("testdata/" + tc.Name)
			if e != nil {
				t.Fatal(e)
			}
			if got := Detect(context.Background(), bytes.NewReader(p), int64(len(p)), tc.Name); got != tc.State {
				t.Fatalf("got %s want %s", got, tc.State)
			}
		})
	}
}
func TestCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p := make([]byte, 512)
	if err := Validate(ctx, bytes.NewReader(p), 512, "a.docx"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestBudgets(t *testing.T) {
	p, _ := os.ReadFile("testdata/normal.doc")
	b := &budget{ctx: context.Background(), r: bytes.NewReader(p), size: int64(len(p)), left: 4, end: time.Now().Add(time.Second)}
	if _, err := b.read(0, 8); err == nil || !b.limited {
		t.Fatal("read budget bypass")
	}
}
func FuzzDetect(f *testing.F) {
	for _, n := range []string{"encrypted.docx", "normal.doc", "cycle.doc", "normal.xlsx"} {
		p, _ := os.ReadFile("testdata/" + n)
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, p []byte) {
		if len(p) > 1<<20 {
			t.Skip()
		}
		Detect(context.Background(), bytes.NewReader(p), int64(len(p)), "fuzz.docx")
	})
}
func BenchmarkDetect(b *testing.B) {
	for _, n := range []string{"encrypted.docx", "normal.xlsx", "normal.doc"} {
		p, _ := os.ReadFile("testdata/" + n)
		b.Run(n, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				Detect(context.Background(), bytes.NewReader(p), int64(len(p)), n)
			}
		})
	}
}
