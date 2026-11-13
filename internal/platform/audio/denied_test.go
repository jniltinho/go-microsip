package audio

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/go-ole/go-ole"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// Windows' refusal of the microphone (E_ACCESSDENIED, as the capture stream reports it) becomes
// the error that names the privacy setting; another failure of the device stays what it was.
func TestAMicrophoneDeniedByWindowsNamesItsSetting(t *testing.T) {
	denied := captureError(fmt.Errorf("open capture: %w", ole.NewError(0x80070005)))
	if !errors.Is(denied, ports.ErrMicrophoneDenied) || !strings.Contains(denied.Error(), "Privacy & security > Microphone") {
		t.Errorf("a denied microphone: %v", denied)
	}
	for _, other := range []error{
		fmt.Errorf("open capture: %w", ole.NewError(0x88890004)), // the device was unplugged
		errors.New("open capture: no such device"),
		nil,
	} {
		if got := captureError(other); got != other {
			t.Errorf("%v became %v", other, got)
		}
	}
}
