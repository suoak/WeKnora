package officeguard

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	werrors "github.com/Tencent/WeKnora/internal/errors"
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
			if tc.State == Encrypted || tc.State == DRM {
				if !errors.As(err, &app) || app.Code != 2400 || app.HTTPCode != 400 {
					t.Fatalf("expected typed rejection, got %v", err)
				}
				if tc.State == DRM && app.Message != DRMMessage {
					t.Fatalf("DRM must explain rights management, got %q", app.Message)
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
	b := &budget{
		ctx:  context.Background(),
		r:    bytes.NewReader(p),
		size: int64(len(p)),
		left: 4,
		end:  time.Now().Add(time.Second),
	}
	if _, err := b.read(0, 8); err == nil || !b.limited {
		t.Fatal("read budget bypass")
	}
}

type officeReaderAtFunc func([]byte, int64) (int, error)

func (f officeReaderAtFunc) ReadAt(p []byte, offset int64) (int, error) { return f(p, offset) }

func TestOfficeReadBudgetRejectsExpiryAfterIO(t *testing.T) {
	b := &budget{ctx: context.Background(), size: 8, left: 8, end: time.Now().Add(time.Hour)}
	b.r = officeReaderAtFunc(func(p []byte, _ int64) (int, error) {
		b.end = time.Now().Add(-time.Second)
		return copy(p, "12345678"), nil
	})
	_, err := b.ReadAt(make([]byte, 8), 0)
	if !errors.Is(err, io.ErrNoProgress) || !b.limited {
		t.Fatalf("completed I/O must not bypass an expired budget: %v, limited=%v", err, b.limited)
	}
}

func TestOfficeCanceledFinalReadCannotReportEncryption(t *testing.T) {
	data, err := os.ReadFile("testdata/encrypted.docx")
	if err != nil {
		t.Fatal(err)
	}
	reads := 0
	reader := bytes.NewReader(data)
	counting := officeReaderAtFunc(func(p []byte, off int64) (int, error) {
		reads++
		return reader.ReadAt(p, off)
	})
	if Detect(context.Background(), counting, int64(len(data)), "test.docx") != Encrypted {
		t.Fatal("invalid encrypted control")
	}
	for _, validate := range []bool{false, true} {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		calls := 0
		interrupted := officeReaderAtFunc(func(p []byte, off int64) (int, error) {
			n, readErr := reader.ReadAt(p, off)
			calls++
			if calls == reads {
				cancel()
			}
			return n, readErr
		})
		if validate {
			if err := Validate(ctx, interrupted, int64(len(data)), "test.docx"); !errors.Is(err, context.Canceled) {
				t.Fatalf("expected cancellation, got %v", err)
			}
		} else if state := Detect(ctx, interrupted, int64(len(data)), "test.docx"); state != Unknown {
			t.Fatalf("canceled final read reported %s", state)
		}
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
