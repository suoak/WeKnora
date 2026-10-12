package sourceid

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"strings"
	"testing"
)

const sampleLogout = `<samlp:LogoutRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" ID="mock-request" Version="2.0" IssueInstant="2026-10-12T00:00:00Z"><samlp:SessionIndex>mock-session</samlp:SessionIndex></samlp:LogoutRequest>`

func TestLogoutParser(t *testing.T) {
	for _, test := range []struct {
		name, raw string
		valid     bool
	}{
		{"valid", sampleLogout, true},
		{"missing index", strings.ReplaceAll(sampleLogout, "<samlp:SessionIndex>mock-session</samlp:SessionIndex>", ""), false},
		{"duplicate index", strings.ReplaceAll(sampleLogout, "</samlp:LogoutRequest>", "<samlp:SessionIndex>other</samlp:SessionIndex></samlp:LogoutRequest>"), false},
		{"wrong namespace", strings.ReplaceAll(sampleLogout, protocolNS, "urn:evil"), false},
		{"XXE", `<!DOCTYPE x [<!ENTITY e SYSTEM "file:///etc/passwd">]>` + sampleLogout, false},
		{"oversize", strings.Repeat("x", MaxLogoutBytes+1), false},
		{"trailing document", sampleLogout + sampleLogout, false},
		{"malformed", sampleLogout[:80], false},
		{"invalid instant", strings.ReplaceAll(sampleLogout, "2026-10-12T00:00:00Z", "invalid"), false},
		{"empty index", strings.ReplaceAll(sampleLogout, "mock-session", " "), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			result, err := ParseLogout(test.raw, "xml")
			if (err == nil) != test.valid {
				t.Fatalf("valid=%v err=%v", test.valid, err)
			}
			if test.valid && result.SessionIndex != "mock-session" {
				t.Fatal("incorrect session")
			}
		})
	}
}

func TestCompressedLogout(t *testing.T) {
	compress := func(value string) string {
		var b bytes.Buffer
		w, _ := flate.NewWriter(&b, flate.BestCompression)
		_, _ = w.Write([]byte(value))
		_ = w.Close()
		return base64.StdEncoding.EncodeToString(b.Bytes())
	}
	if _, err := ParseLogout(compress(sampleLogout), "deflate-base64"); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"not-base64", base64.StdEncoding.EncodeToString([]byte("invalid")), compress(strings.Repeat("x", MaxLogoutBytes+1))} {
		if _, err := ParseLogout(raw, "deflate-base64"); err == nil {
			t.Fatal("invalid compressed message accepted")
		}
	}
	if _, err := ParseLogout(compress(sampleLogout), "xml"); err == nil {
		t.Fatal("encoding guessed")
	}
}
