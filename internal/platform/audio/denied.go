package audio

import (
	"errors"
	"fmt"

	"github.com/go-ole/go-ole"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// eAccessDenied is what Windows answers a capture stream when its microphone privacy setting
// keeps desktop applications out (E_ACCESSDENIED).
const eAccessDenied = 0x80070005

// captureError puts a failure to open the microphone in the core's terms: a denial by Windows'
// privacy setting is ports.ErrMicrophoneDenied, which names the setting; anything else stays as
// it is.
func captureError(err error) error {
	var failure *ole.OleError
	if errors.As(err, &failure) && uint32(failure.Code()) == eAccessDenied {
		return fmt.Errorf("%w (%v)", ports.ErrMicrophoneDenied, err)
	}
	return err
}
