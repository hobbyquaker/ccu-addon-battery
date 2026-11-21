// Package rega is what the addon needs of a classic CCU's ReGaHSS beyond the kit's source: the
// text of its answers, and the per-device switch of the service messages.
//
// ReGa keeps every text as the bytes the WebUI sent it, and the WebUI's pages are ISO-8859-1
// (browsers take that as Windows-1252): a device named "Türe" is T, 0xFC, r, e in rega.exe's
// answer, with no charset declared. Taken as UTF-8 that byte is invalid, and the name went out
// as "T�re". Transport turns the answers into UTF-8 before anybody reads them.
package rega

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"
)

// cp1252 are the characters of 0x80-0x9F in Windows-1252; the five bytes it leaves undefined
// stay the C1 control characters ISO-8859-1 has there.
var cp1252 = [32]rune{
	0x20AC, 0x0081, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021, 0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0x008D, 0x017D, 0x008F,
	0x0090, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0x009D, 0x017E, 0x0178,
}

// Decode turns ReGa's text into UTF-8, line by line: a line that is not valid UTF-8 is
// ISO-8859-1 (Windows-1252), a line that is valid stays as it is - ASCII, and the names a
// program wrote into ReGa as UTF-8. A text that is UTF-8 already comes back unchanged, so
// decoding twice does no harm.
func Decode(b []byte) []byte {
	if utf8.Valid(b) {
		return b
	}
	var out bytes.Buffer
	out.Grow(len(b) + len(b)/8)
	for len(b) > 0 {
		line := b
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			line = b[:i+1]
		}
		b = b[len(line):]
		if utf8.Valid(line) {
			out.Write(line)
			continue
		}
		for _, c := range line {
			switch {
			case c < 0x80:
				out.WriteByte(c)
			case c < 0xA0:
				out.WriteRune(cp1252[c-0x80])
			default:
				out.WriteRune(rune(c))
			}
		}
	}
	return out.Bytes()
}

// maxAnswer bounds an answer of rega.exe that is decoded (the kit reads at most as much).
const maxAnswer = 16 << 20

// Transport is an http.RoundTripper that decodes the answers of rega.exe (a request whose path
// ends in /rega.exe) and passes everything else - the interface processes' XML-RPC, which
// declares its encoding - through untouched.
type Transport struct {
	// Base does the requests; http.DefaultTransport when nil.
	Base http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (t Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	res, err := base.RoundTrip(req)
	if err != nil || !strings.HasSuffix(req.URL.Path, "/rega.exe") {
		return res, err
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, maxAnswer))
	res.Body.Close()
	if err != nil {
		return nil, err
	}
	b = Decode(b)
	res.Body = io.NopCloser(bytes.NewReader(b))
	res.ContentLength = int64(len(b))
	res.Header.Set("Content-Length", strconv.Itoa(len(b)))
	return res, nil
}
