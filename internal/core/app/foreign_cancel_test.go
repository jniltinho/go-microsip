package app

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/domain"
	"github.com/jniltinho/go-microsip/internal/core/ports/memory"
)

// A CANCEL that matches a ringing call and comes from another address than the registrar's does
// nothing: the call rings on. The SIP stack matches a CANCEL by its Via and acts on it before any
// request handler runs, so the stack is given only what the registrar sends, on every socket it
// reads. The registrar's own CANCEL then ends the call.
//
// Through the loopback the softphone listens on every interface and the SIP library opens a
// socket of its own for what it sends: the INVITE and both CANCELs go to that one.
func TestACancelFromAnotherAddressDoesNotEndARingingCall(t *testing.T) {
	here := net.IPv4(127, 0, 0, 1)
	registrar, err := net.ListenUDP("udp", &net.UDPAddr{IP: here})
	if err != nil {
		t.Fatal(err)
	}
	defer registrar.Close()
	port := registrar.LocalAddr().(*net.UDPAddr).Port
	softphone := make(chan *net.UDPAddr, 1)
	go func() { // accepts the registration and ignores everything else the softphone says
		buf := make([]byte, 65535)
		for {
			n, from, err := registrar.ReadFromUDP(buf)
			if err != nil {
				return
			}
			message := string(buf[:n])
			if !strings.HasPrefix(message, "REGISTER ") {
				continue
			}
			var out strings.Builder
			out.WriteString("SIP/2.0 200 OK\r\n")
			for _, line := range strings.Split(message, "\r\n")[1:] {
				if line == "" {
					break
				}
				for _, name := range []string{"Via:", "From:", "Call-ID:", "CSeq:", "Contact:"} {
					if strings.HasPrefix(line, name) {
						out.WriteString(line + "\r\n")
					}
				}
				if strings.HasPrefix(line, "To:") {
					out.WriteString(line + ";tag=pbx\r\n")
				}
			}
			out.WriteString("Expires: 60\r\nContent-Length: 0\r\n\r\n")
			_, _ = registrar.WriteToUDP([]byte(out.String()), from)
			select {
			case softphone <- from:
			default:
			}
		}
	}()
	dir := t.TempDir()
	file := filepath.Join(dir, "config.toml")
	text := fmt.Sprintf("schema = 1\n[account]\nusername = \"1001\"\ndomain = %q\nport = %d\ntransport = \"udp\"\nexpires = 60\n", here.String(), port)
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	o := Options{ConfigPath: file, Files: own{}, Env: map[string]string{"XDG_DATA_HOME": filepath.Join(dir, "data"), "GO_MICROSIP_SIP_PASSWORD": "lab"}}
	p, err := OpenPhone(o, PhoneOptions{Audio: &memory.Audio{Tone: 1000}})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	until(t, p, "registration", registered)
	to := <-softphone

	request := func(method, extra, body string) []byte {
		return []byte(fmt.Sprintf("%[1]s sip:1001@%[2]s SIP/2.0\r\nVia: SIP/2.0/UDP %[3]s:%[4]d;branch=z9hG4bK-lab-ringing;rport\r\nMax-Forwards: 70\r\n"+
			"From: <sip:2000@%[3]s>;tag=pbx-leg\r\nTo: <sip:1001@%[3]s>\r\nCall-ID: a-call-that-rings\r\nCSeq: 1 %[1]s\r\n%[5]sContent-Length: %[6]d\r\n\r\n%[7]s",
			method, to, here, port, extra, len(body), body))
	}
	offer := fmt.Sprintf("v=0\r\no=- 1 1 IN IP4 %[1]s\r\ns=-\r\nc=IN IP4 %[1]s\r\nt=0 0\r\nm=audio 40000 RTP/AVP 0 101\r\na=rtpmap:0 PCMU/8000\r\na=rtpmap:101 telephone-event/8000\r\na=sendrecv\r\n", here)
	invite := request("INVITE", fmt.Sprintf("Contact: <sip:2000@%s:%d>\r\nContent-Type: application/sdp\r\n", here, port), offer)
	if _, err := registrar.WriteToUDP(invite, to); err != nil {
		t.Fatal(err)
	}
	until(t, p, "the call ringing", func(ev domain.Event) bool {
		c, ok := ev.(domain.CallChanged)
		return ok && c.Call.Direction == domain.Incoming && c.Call.Phase.Kind == domain.Ringing
	})

	// Another host on the network that has seen the INVITE: the same Via, Call-ID and CSeq number,
	// and a reason that would also hide the call from the missed ones.
	intruder, err := net.ListenUDP("udp", &net.UDPAddr{IP: here})
	if err != nil {
		t.Fatal(err)
	}
	defer intruder.Close()
	if _, err := intruder.WriteToUDP(request("CANCEL", "Reason: SIP;cause=200;text=\"Call completed elsewhere\"\r\n", ""), to); err != nil {
		t.Fatal(err)
	}
	quiet := time.After(700 * time.Millisecond)
waiting:
	for {
		select {
		case ev := <-p.Events():
			if c, ok := ev.(domain.CallChanged); ok && c.Call.Phase.Kind != domain.Ringing {
				t.Fatalf("a CANCEL from another address made the ringing call %s", c.Call.Phase)
			}
		case <-quiet:
			break waiting
		}
	}
	// The registrar's own CANCEL is obeyed.
	if _, err := registrar.WriteToUDP(request("CANCEL", "", ""), to); err != nil {
		t.Fatal(err)
	}
	until(t, p, "the call's end", func(ev domain.Event) bool {
		c, ok := ev.(domain.CallChanged)
		if ok && c.Call.Phase.IsFinal() && c.Call.Phase.End != domain.EndCanceled {
			t.Errorf("the call the registrar canceled ended as %s", c.Call.Phase.End)
		}
		return ok && c.Call.Phase.IsFinal()
	})
}
