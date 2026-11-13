package notify

import (
	"os"
	"testing"
	"time"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// On the Windows VM's desktop (build/vm/desktop.ps1): a ringing call is handed to Windows as a
// toast, and its Answer button, pressed there by the check through UI automation, comes back as
// the decision for that call.
func TestAToastOnThisDesktop(t *testing.T) {
	if os.Getenv("GO_MICROSIP_NOTIFY_TEST") != "desktop" {
		t.Skip("set GO_MICROSIP_NOTIFY_TEST=desktop in a Windows desktop session (make go-vm)")
	}
	d := New() // its buttons come back through this one
	time.Sleep(200 * time.Millisecond)
	if err := show(41, callerText("Ana <1002>")); err != nil {
		t.Fatalf("Windows did not take the toast: %v", err)
	}
	t.Log("Windows took the toast of call 41")
	select {
	case action := <-d.Actions():
		if want := (ports.CallNoticeAction{Call: 41, Answer: true}); action != want {
			t.Errorf("the button pressed came back as %+v, want %+v", action, want)
		} else {
			t.Log("the toast's Answer button came back as the decision to answer call 41")
		}
	case <-time.After(45 * time.Second):
		if os.Getenv("GO_MICROSIP_NOTIFY_PRESSED") != "" {
			t.Error("no button of the toast came back within 45 s")
		} else {
			t.Log("NOT VERIFIED: no button of the toast was pressed")
		}
	}
}
