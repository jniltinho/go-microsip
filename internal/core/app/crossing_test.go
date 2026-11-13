package app

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports/memory"
)

// crossingPBX is a PBX over UDP that answers a call at the very moment it is canceled: it says
// 200 to the CANCEL and then 200 to the INVITE, as a PBX does whose other phone picked up while
// the cancellation was on its way. It records what the softphone sends after that.
type crossingPBX struct {
	port int
	mu   sync.Mutex
	got  []string // the methods received, in order
}

func (p *crossingPBX) methods() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.got...)
}

func newCrossingPBX(t *testing.T) *crossingPBX {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	p := &crossingPBX{port: conn.LocalAddr().(*net.UDPAddr).Port}
	header := func(message, name string) string {
		for _, line := range strings.Split(message, "\r\n")[1:] {
			if rest, ok := strings.CutPrefix(line, name+":"); ok {
				return strings.TrimSpace(rest)
			}
			if line == "" {
				break
			}
		}
		return ""
	}
	respond := func(to *net.UDPAddr, request string, cseq string, status string, extra string, body string) {
		toHeader := header(request, "To")
		if !strings.Contains(toHeader, "tag=") {
			toHeader += ";tag=pbx"
		}
		var out strings.Builder
		fmt.Fprintf(&out, "SIP/2.0 %s\r\n", status)
		for _, line := range strings.Split(request, "\r\n")[1:] {
			if strings.HasPrefix(line, "Via:") {
				out.WriteString(line + "\r\n")
			}
		}
		fmt.Fprintf(&out, "From: %s\r\nTo: %s\r\nCall-ID: %s\r\nCSeq: %s\r\n%sContent-Length: %d\r\n\r\n%s",
			header(request, "From"), toHeader, header(request, "Call-ID"), cseq, extra, len(body), body)
		_, _ = conn.WriteToUDP([]byte(out.String()), to)
	}
	go func() {
		buf := make([]byte, 65535)
		var invite string
		for {
			n, from, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			message := string(buf[:n])
			method, _, _ := strings.Cut(message, " ")
			if strings.HasPrefix(method, "SIP/") {
				continue // a response of the softphone's
			}
			p.mu.Lock()
			p.got = append(p.got, method)
			p.mu.Unlock()
			contact := fmt.Sprintf("Contact: <sip:pbx@127.0.0.1:%d>\r\n", p.port)
			switch method {
			case "REGISTER":
				respond(from, message, header(message, "CSeq"), "200 OK", fmt.Sprintf("Contact: %s;expires=60\r\n", header(message, "Contact")), "")
			case "INVITE":
				invite = message
				respond(from, message, header(message, "CSeq"), "180 Ringing", contact, "")
			case "CANCEL":
				respond(from, message, header(message, "CSeq"), "200 OK", "", "")
				// The answer that crossed the cancellation.
				sdp := "v=0\r\no=- 1 1 IN IP4 127.0.0.1\r\ns=-\r\nc=IN IP4 127.0.0.1\r\nt=0 0\r\nm=audio 40000 RTP/AVP 0 101\r\na=rtpmap:0 PCMU/8000\r\na=rtpmap:101 telephone-event/8000\r\na=sendrecv\r\n"
				respond(from, invite, header(invite, "CSeq"), "200 OK", contact+"Content-Type: application/sdp\r\n", sdp)
			case "BYE":
				respond(from, message, header(message, "CSeq"), "200 OK", "", "")
			}
		}
	}()
	return p
}

// A hang-up that crosses the answer does not leave the other side in a call: when the PBX
// answers the call that is being canceled, the softphone acknowledges the answer and hangs the
// call up, and its own call ends as canceled.
func TestAnAnswerThatCrossesTheHangUpIsHungUp(t *testing.T) {
	pbx := newCrossingPBX(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1001\"\ndomain = \"127.0.0.1\"\nport = %d\ntransport = \"udp\"\nexpires = 60\n", pbx.port)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	o := Options{ConfigPath: file, Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data"), "GO_MICROSIP_SIP_PASSWORD": "lab"}}
	sound := &memory.Audio{Tone: 1000}
	p, err := OpenPhone(o, PhoneOptions{Audio: sound})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	until(t, p, "registration", registered)
	dial(t, p)
	until(t, p, "the call ringing", func(ev domain.Event) bool {
		c, ok := ev.(domain.CallChanged)
		return ok && c.Call.Phase.Kind == domain.Ringing
	})
	if err := p.Send(domain.Hangup{Call: 1}); err != nil {
		t.Fatal(err)
	}
	until(t, p, "the call's end", func(ev domain.Event) bool {
		c, ok := ev.(domain.CallChanged)
		if ok && c.Call.Phase.IsFinal() && c.Call.Phase.End != domain.EndCanceled {
			t.Errorf("the call ended as %s, want canceled", c.Call.Phase.End)
		}
		return ok && c.Call.Phase.IsFinal()
	})
	soon(t, "the answer acknowledged and the call hung up", func() bool {
		got := strings.Join(pbx.methods(), " ")
		return strings.Contains(got, "CANCEL") && strings.Contains(got, "ACK") && strings.HasSuffix(got, "BYE")
	})
	if n := sound.CallStreams(); n != 0 {
		t.Errorf("%d sound streams were opened for the call that was given up", n)
	}
}
