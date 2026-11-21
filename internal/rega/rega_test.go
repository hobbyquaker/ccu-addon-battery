package rega

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hobbyquaker/ccu-addon-kit/go/ccusource"
)

// latin1 encodes a text the way ReGa answers it: one byte per character.
func latin1(t *testing.T, s string) []byte {
	t.Helper()
	b := make([]byte, 0, len(s))
	for _, r := range s {
		if r > 0xFF {
			t.Fatalf("%q is not ISO-8859-1", r)
		}
		b = append(b, byte(r))
	}
	return b
}

func TestDecode(t *testing.T) {
	for _, c := range []struct {
		name string
		in   []byte
		want string
	}{
		{"ascii", []byte("D\tHmIP-RF\tDEVA\tTaster\n"), "D\tHmIP-RF\tDEVA\tTaster\n"},
		{"the report", latin1(t, "Türe Waschkeller\nTüre Geräteschuppen\n"), "Türe Waschkeller\nTüre Geräteschuppen\n"},
		{"every umlaut", latin1(t, "ä ö ü ß Ä Ö Ü"), "ä ö ü ß Ä Ö Ü"},
		{"utf-8 already", []byte("Türe Geräteschuppen ä ö ü ß Ä Ö Ü\n"), "Türe Geräteschuppen ä ö ü ß Ä Ö Ü\n"},
		{"a utf-8 line among latin-1 ones", append(append(latin1(t, "Küche\n"), []byte("Büro\n")...), latin1(t, "Straße")...), "Küche\nBüro\nStraße"},
		{"windows-1252", []byte{'5', ' ', 0x80, ' ', 0x96, ' ', 0x84, 'x', 0x93}, "5 € – „x“"},
		{"other latin-1", latin1(t, "café °C µ"), "café °C µ"},
		{"empty", nil, ""},
	} {
		got := string(Decode(c.in))
		if got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
		if again := string(Decode([]byte(got))); again != got {
			t.Errorf("%s: decoding twice changed %q to %q", c.name, got, again)
		}
	}
}

// The names of a classic CCU, read by the kit's source through the transport: ReGa answers
// ISO-8859-1, the names and rooms come out as UTF-8. (Without the transport the kit takes the
// bytes as UTF-8 and the JSON encoder turns each umlaut into U+FFFD.)
func TestNamesFromRega(t *testing.T) {
	const script = "D\tBidCos-RF\tJEQ01\tTüre Geräteschuppen\n" +
		"C\tJEQ01:0\tTüre Geräteschuppen:0\t1234|\n" +
		"C\tJEQ01:1\tä ö ü ß Ä Ö Ü\t1234|\n" +
		"D\tHmIP-RF\tDEVA\tGroßes Fenster\n" +
		"R\t1234\tKüche\n"
	var posted string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		posted = string(b)
		w.Header().Set("Content-Type", "text/xml; charset=iso-8859-1") // as rega.exe answers
		w.Write(latin1(t, script))
		io.WriteString(w, "<xml><exec>/rega.exe</exec></xml>")
	}))
	defer srv.Close()
	for _, c := range []struct {
		name      string
		transport http.RoundTripper
		want      bool
	}{
		{"with the transport", Transport{}, true},
		{"without", nil, false},
	} {
		s := ccusource.New(ccusource.Options{ID: "battery", Rega: srv.URL + "/rega.exe", Client: &http.Client{Timeout: 5 * time.Second, Transport: c.transport}, Log: t.Logf})
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		n, err := s.Names(ctx)
		cancel()
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !strings.Contains(posted, "EnumUsedIDs") {
			t.Fatalf("%s: the script was not posted: %q", c.name, posted)
		}
		got := []string{n.Name("BidCos-RF", "JEQ01"), n.Name("BidCos-RF", "JEQ01:1"), n.Name("HmIP-RF", "DEVA"), strings.Join(n.RoomNames("BidCos-RF", "JEQ01", "de"), ",")}
		want := []string{"Türe Geräteschuppen", "ä ö ü ß Ä Ö Ü", "Großes Fenster", "Küche"}
		ok := strings.Join(got, "|") == strings.Join(want, "|")
		if ok != c.want {
			// "without" fails the day the kit decodes ReGa's answers itself: then the
			// transport is no longer needed (it does no harm - Decode leaves UTF-8 alone)
			t.Errorf("%s: names %q, right = %v, want right = %v", c.name, got, ok, c.want)
		}
	}
}

// Everything that is not rega.exe passes untouched: the interface processes declare their
// encoding and the kit's XML-RPC client decodes it.
func TestTransportLeavesOthersAlone(t *testing.T) {
	raw := []byte{'<', 'v', '>', 0xFC, '<', '/', 'v', '>'}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(raw) }))
	defer srv.Close()
	c := &http.Client{Transport: Transport{}}
	for path, want := range map[string]string{"/": string(raw), "/rega.exe": "<v>ü</v>"} {
		res, err := c.Post(srv.URL+path, "text/xml", strings.NewReader("x"))
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if string(b) != want {
			t.Errorf("%s: %q, want %q", path, b, want)
		}
	}
}

func TestParseServiceOff(t *testing.T) {
	out := "S\tJEQ01\tnull\r\nS\tJEQ02\tfalse\r\nS\tJEQ03\ttrue\r\nS\t\tfalse\r\njunk\nS\t000A1\tfalse\n<xml><exec>/rega.exe</exec></xml>"
	got := ParseServiceOff(out)
	if len(got) != 2 || !got["JEQ02"] || !got["000A1"] {
		t.Fatalf("off: %v", got)
	}
	if len(ParseServiceOff("")) != 0 {
		t.Fatal("empty")
	}
	if !strings.Contains(ServiceOffScript, `MetaData("enabledServiceMsg")`) {
		t.Fatal("the script reads the WebUI's switch")
	}
}
