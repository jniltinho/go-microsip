package control

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// On a desktop's own session (the VM check, build/vm/check.sh): the go-microsip window running
// there answers a client of the same session; and the Rust candidate, when it runs beside it,
// has an endpoint of its own that does not take go-microsip's hello (task 6.4, coexistence).
func TestTheWindowOfThisSessionAnswers(t *testing.T) {
	if os.Getenv("GO_MICROSIP_CONTROL_TEST") != "session" {
		t.Skip("set GO_MICROSIP_CONTROL_TEST=session in a session with a go-microsip window (make go-vm)")
	}
	session := os.Getenv("XDG_RUNTIME_DIR")
	dir, err := RuntimeDir(map[string]string{"XDG_RUNTIME_DIR": session})
	if err != nil {
		t.Fatal(err)
	}
	client, err := Dial(dir)
	if err != nil {
		t.Fatalf("the window of this session does not answer: %v", err)
	}
	defer client.Close()
	answer := client.Do(Request{SchemaVersion: SchemaVersion, RequestID: "vm-1", Operation: Operation{Op: StatusGet}})
	var data struct {
		Mode         Mode   `json:"mode"`
		Epoch        int64  `json:"epoch"`
		Registration string `json:"registration"`
	}
	if answer.Status != StatusOK || json.Unmarshal(answer.Data, &data) != nil || data.Mode != ModeWindow || data.Epoch == 0 {
		t.Fatalf("status_get: %+v (%+v)", answer, answer.Error)
	}
	t.Logf("go-microsip's window answers on its own socket: protocol %s, epoch %d, registration %s", client.Welcome().Protocol, data.Epoch, data.Registration)
	if r := client.Do(Request{SchemaVersion: SchemaVersion, RequestID: "vm-2", Operation: Operation{Op: HistoryList}}); r.Error == nil || r.Error.Code != GrantRequired {
		t.Errorf("history without a grant: %+v", r)
	}
	rust := filepath.Join(session, "rust-microsip")
	if _, err := os.Stat(filepath.Join(rust, SocketFile)); err != nil {
		if os.Getenv("GO_MICROSIP_CONTROL_RUST") != "" {
			t.Errorf("the Rust candidate's socket is not there: %v", err)
		}
		return
	}
	if _, err := Dial(rust); !errors.Is(err, ErrProtocol) {
		t.Errorf("go-microsip's hello at the Rust candidate's socket: %v, want a refusal of the protocol", err)
	} else {
		t.Log("the Rust candidate's owner, on its own socket, does not take go-microsip's hello")
	}
}
