// Package sourceid contains SourceID-specific wire parsing, not OIDC JWT logout.
package sourceid

import (
	"bytes"
	"compress/flate"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"io"
	"strings"
	"time"
)

const MaxLogoutBytes = 64 << 10
const protocolNS = "urn:oasis:names:tc:SAML:2.0:protocol"

type LogoutRequest struct {
	ID           string
	SessionIndex string
	IssueInstant time.Time
}

// Encoding is explicit: compressed traffic is disabled until independently
// confirmed. Both compressed and expanded messages have strict size limits.
func ParseLogout(raw, encoding string) (*LogoutRequest, error) {
	if len(raw) == 0 || len(raw) > MaxLogoutBytes {
		return nil, errors.New("invalid logout size")
	}
	data := []byte(raw)
	switch encoding {
	case "", "xml":
	case "deflate-base64":
		decoded, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			return nil, errors.New("invalid logout encoding")
		}
		r := flate.NewReader(bytes.NewReader(decoded))
		defer r.Close()
		data, err = io.ReadAll(io.LimitReader(r, MaxLogoutBytes+1))
		if err != nil || len(data) > MaxLogoutBytes {
			return nil, errors.New("invalid compressed logout")
		}
	default:
		return nil, errors.New("unsupported logout encoding")
	}
	d := xml.NewDecoder(bytes.NewReader(data))
	depth, roots := 0, 0
	result := &LogoutRequest{}
	var capture bool
	var text strings.Builder
	for {
		token, err := d.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.New("invalid logout XML")
		}
		switch t := token.(type) {
		case xml.Directive:
			return nil, errors.New("XML directives prohibited")
		case xml.StartElement:
			depth++
			if depth > 8 {
				return nil, errors.New("XML too deep")
			}
			if depth == 1 {
				roots++
				if roots != 1 || t.Name.Local != "LogoutRequest" || t.Name.Space != protocolNS {
					return nil, errors.New("invalid logout root")
				}
				version := ""
				for _, a := range t.Attr {
					if a.Name.Space != "" {
						continue
					}
					switch a.Name.Local {
					case "ID":
						result.ID = a.Value
					case "Version":
						version = a.Value
					case "IssueInstant":
						result.IssueInstant, err = time.Parse(time.RFC3339, a.Value)
						if err != nil {
							return nil, errors.New("invalid logout time")
						}
					}
				}
				if version != "2.0" {
					return nil, errors.New("invalid logout version")
				}
			}
			if capture {
				return nil, errors.New("nested SessionIndex")
			}
			if t.Name.Local == "SessionIndex" {
				if depth != 2 || t.Name.Space != protocolNS || result.SessionIndex != "" {
					return nil, errors.New("invalid SessionIndex")
				}
				capture = true
				text.Reset()
			}
		case xml.CharData:
			if capture {
				text.Write([]byte(t))
			} else if depth == 0 && strings.TrimSpace(string(t)) != "" {
				return nil, errors.New("trailing XML")
			}
		case xml.EndElement:
			if capture && t.Name.Local == "SessionIndex" {
				result.SessionIndex = strings.TrimSpace(text.String())
				if result.SessionIndex == "" {
					return nil, errors.New("empty SessionIndex")
				}
				capture = false
			}
			depth--
		}
	}
	if roots != 1 || depth != 0 || result.ID == "" || len(result.ID) > 512 || result.SessionIndex == "" || len(result.SessionIndex) > 512 || result.IssueInstant.IsZero() {
		return nil, errors.New("missing logout fields")
	}
	return result, nil
}
