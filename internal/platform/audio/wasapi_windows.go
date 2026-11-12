package audio

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
	"github.com/moutend/go-wca/pkg/wca"
)

// The Windows backend is WASAPI in shared mode, reached through COM from Go: no C compiler. Windows
// converts between the device's format and the 8 kHz mono frames of this package.

const (
	// poll is how often each device thread moves samples.
	poll = 10 * time.Millisecond
	// deviceBuffer is the endpoint buffer asked of Windows, in 100 ns units (100 ms).
	deviceBuffer = wca.REFERENCE_TIME(1_000_000)
	// playQueue is the most audio kept queued in the device: 60 ms.
	playQueue = 3 * FrameSamples
)

// format is what both streams exchange with Windows: 16-bit mono PCM at SampleRate.
var format = wca.WAVEFORMATEX{
	WFormatTag: 1, NChannels: 1, NSamplesPerSec: SampleRate,
	NAvgBytesPerSec: SampleRate * 2, NBlockAlign: 2, WBitsPerSample: 16,
}

var zeros [ringSamples]int16

// rpcEChangedMode is RPC_E_CHANGED_MODE: the thread already has COM in the other threading mode.
const rpcEChangedMode = 0x80010106

// onCOM runs f on one operating-system thread with COM initialized, as every WASAPI call needs.
func onCOM(f func() error) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED); err != nil {
		oe, ok := err.(*ole.OleError)
		switch {
		case ok && oe.Code() == 1:
			// S_FALSE: this thread already has COM in this mode; the call still counts.
		case ok && oe.Code() == rpcEChangedMode:
			// Whoever owns this thread set COM up the other way (the window's thread is one) and
			// that cannot be changed. WASAPI works in either mode, and there is nothing to undo.
			return f()
		default:
			return err
		}
	}
	defer ole.CoUninitialize()
	return f()
}

func enumerator() (*wca.IMMDeviceEnumerator, error) {
	var e *wca.IMMDeviceEnumerator
	err := wca.CoCreateInstance(wca.CLSID_MMDeviceEnumerator, 0, wca.CLSCTX_ALL, wca.IID_IMMDeviceEnumerator, &e)
	return e, err
}

// Client is the session's audio service.
type Client struct{}

// Connect reaches the Windows audio service, or fails with ErrNoService.
func Connect() (*Client, error) {
	err := onCOM(func() error {
		e, err := enumerator()
		if err != nil {
			return err
		}
		e.Release()
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNoService, err)
	}
	return &Client{}, nil
}

// Close releases the client; streams are closed on their own.
func (c *Client) Close() {}

// Inputs lists the capture devices.
func (c *Client) Inputs() ([]Device, error) { return list(wca.ECapture) }

// Outputs lists the playback devices.
func (c *Client) Outputs() ([]Device, error) { return list(wca.ERender) }

func list(flow uint32) ([]Device, error) {
	var out []Device
	err := onCOM(func() error {
		e, err := enumerator()
		if err != nil {
			return err
		}
		defer e.Release()
		def := ""
		var d *wca.IMMDevice
		if e.GetDefaultAudioEndpoint(flow, wca.ECommunications, &d) == nil {
			_ = d.GetId(&def)
			d.Release()
		}
		var col *wca.IMMDeviceCollection
		if err := e.EnumAudioEndpoints(flow, wca.DEVICE_STATE_ACTIVE, &col); err != nil {
			return err
		}
		defer col.Release()
		var n uint32
		if err := col.GetCount(&n); err != nil {
			return err
		}
		for i := range n {
			var d *wca.IMMDevice
			if col.Item(i, &d) != nil {
				continue
			}
			var id string
			if d.GetId(&id) == nil {
				out = append(out, Device{ID: id, Name: friendlyName(d), Default: id == def})
			}
			d.Release()
		}
		return nil
	})
	return out, err
}

func friendlyName(d *wca.IMMDevice) string {
	var ps *wca.IPropertyStore
	if d.OpenPropertyStore(wca.STGM_READ, &ps) != nil {
		return ""
	}
	defer ps.Release()
	var pv wca.PROPVARIANT
	if ps.GetValue(&wca.PKEY_Device_FriendlyName, &pv) != nil {
		return ""
	}
	return pv.String()
}

// find returns the endpoint with the ID, or the default communications endpoint for an empty ID.
func find(e *wca.IMMDeviceEnumerator, flow uint32, id string) (*wca.IMMDevice, error) {
	var d *wca.IMMDevice
	if id == "" {
		return d, e.GetDefaultAudioEndpoint(flow, wca.ECommunications, &d)
	}
	var col *wca.IMMDeviceCollection
	if err := e.EnumAudioEndpoints(flow, wca.DEVICE_STATE_ACTIVE, &col); err != nil {
		return nil, err
	}
	defer col.Release()
	var n uint32
	_ = col.GetCount(&n)
	for i := range n {
		if col.Item(i, &d) != nil {
			continue
		}
		var got string
		if d.GetId(&got) == nil && got == id {
			return d, nil
		}
		d.Release()
	}
	return nil, fmt.Errorf("no active device %q", id)
}

// openClient activates and initializes an audio client on an endpoint. Callers are on a COM thread.
func openClient(flow uint32, id string, flags uint32) (*wca.IAudioClient, error) {
	e, err := enumerator()
	if err != nil {
		return nil, err
	}
	defer e.Release()
	d, err := find(e, flow, id)
	if err != nil {
		return nil, err
	}
	defer d.Release()
	var ac *wca.IAudioClient
	if err := d.Activate(wca.IID_IAudioClient, wca.CLSCTX_ALL, nil, &ac); err != nil {
		return nil, err
	}
	f := format
	flags |= wca.AUDCLNT_STREAMFLAGS_AUTOCONVERTPCM | wca.AUDCLNT_STREAMFLAGS_SRC_DEFAULT_QUALITY
	if err := ac.Initialize(wca.AUDCLNT_SHAREMODE_SHARED, flags, deviceBuffer, 0, &f, nil); err != nil {
		ac.Release()
		return nil, err
	}
	return ac, nil
}

// Stream is one call's audio: a capture and a playback stream on chosen devices. A stream is bound
// to its endpoints: Windows never moves it to another device.
type Stream struct {
	*buffers
	closed, lost, playLost atomic.Bool
	wg                     sync.WaitGroup
}

// Open starts capture on the input and playback on the output, by device ID; an empty ID is the
// default communications device.
func (c *Client) Open(input, output string) (*Stream, error) {
	s := &Stream{buffers: newBuffers()}
	ready := make(chan error, 2)
	s.wg.Add(2)
	go s.capture(wca.ECapture, input, 0, ready)
	go s.render(output, ready)
	var first error
	for range 2 {
		if err := <-ready; err != nil && first == nil {
			first = err
		}
	}
	if first != nil {
		s.Close()
		return nil, first
	}
	return s, nil
}

// OpenOutput starts playback alone on the output, by device ID, for a ring; an empty ID is the
// default communications device. No microphone is opened.
func (c *Client) OpenOutput(output string) (*Stream, error) {
	s := &Stream{buffers: newBuffers()}
	ready := make(chan error, 1)
	s.wg.Add(1)
	go s.render(output, ready)
	if err := <-ready; err != nil {
		s.Close()
		return nil, err
	}
	return s, nil
}

// capture is the device thread of capture: it only copies packets into the ring. It reports once
// on ready whether the stream started, and marks the microphone lost if the device fails later.
func (s *Stream) capture(flow uint32, id string, flags uint32, ready chan<- error) {
	defer s.wg.Done()
	started := false
	err := onCOM(func() error {
		ac, err := openClient(flow, id, flags)
		if err != nil {
			return fmt.Errorf("open capture: %w", err)
		}
		defer ac.Release()
		var cc *wca.IAudioCaptureClient
		if err := ac.GetService(wca.IID_IAudioCaptureClient, &cc); err != nil {
			return fmt.Errorf("open capture: %w", err)
		}
		defer cc.Release()
		if err := ac.Start(); err != nil {
			return fmt.Errorf("start capture: %w", err)
		}
		defer ac.Stop()
		started = true
		ready <- nil
		for !s.closed.Load() {
			time.Sleep(poll)
			for {
				var next uint32
				if err := cc.GetNextPacketSize(&next); err != nil {
					return err
				}
				if next == 0 {
					// Nothing is in this thread's hands and the device has handed over all it
					// had: a discard that waits is behind.
					s.discardEnd(0)
					break
				}
				var data *byte
				var frames, bufFlags uint32
				if err := cc.GetBuffer(&data, &frames, &bufFlags, nil, nil); err != nil {
					return err
				}
				if bufFlags&wca.AUDCLNT_BUFFERFLAGS_SILENT != 0 || data == nil {
					s.captured(zeros[:min(int(frames), len(zeros))])
				} else {
					s.captured(unsafe.Slice((*int16)(unsafe.Pointer(data)), frames))
				}
				if err := cc.ReleaseBuffer(frames); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if !started {
		ready <- err
	} else if err != nil {
		s.lost.Store(true) // the device was unplugged or the service stopped
	}
}

// render is the device thread of playback: it only copies from the ring into the device buffer,
// keeping at most playQueue queued there.
func (s *Stream) render(id string, ready chan<- error) {
	defer s.wg.Done()
	started := false
	err := onCOM(func() error {
		ac, err := openClient(wca.ERender, id, 0)
		if err != nil {
			return fmt.Errorf("open playback: %w", err)
		}
		defer ac.Release()
		var rc *wca.IAudioRenderClient
		if err := ac.GetService(wca.IID_IAudioRenderClient, &rc); err != nil {
			return fmt.Errorf("open playback: %w", err)
		}
		defer rc.Release()
		var size uint32
		if err := ac.GetBufferSize(&size); err != nil {
			return fmt.Errorf("open playback: %w", err)
		}
		if err := ac.Start(); err != nil {
			return fmt.Errorf("start playback: %w", err)
		}
		defer ac.Stop()
		started = true
		ready <- nil
		for !s.closed.Load() {
			time.Sleep(poll)
			var queued uint32
			if err := ac.GetCurrentPadding(&queued); err != nil {
				return err
			}
			want := min(int(size)-int(queued), playQueue-int(queued), s.out.len())
			if want <= 0 {
				if queued == 0 {
					// Nothing queued and nothing to queue: the device plays silence this period.
					s.silence.Add(SampleRate * uint64(poll) / uint64(time.Second))
				}
				continue
			}
			var data *byte
			if err := rc.GetBuffer(uint32(want), &data); err != nil {
				return err
			}
			n := s.out.read(unsafe.Slice((*int16)(unsafe.Pointer(data)), want))
			if err := rc.ReleaseBuffer(uint32(n), 0); err != nil {
				return err
			}
		}
		return nil
	})
	if !started {
		ready <- err
	} else if err != nil {
		s.playLost.Store(true) // the device was unplugged or the service stopped
	}
}

// PlaybackLost reports that the speaker stream ended: the device or the service is gone.
func (s *Stream) PlaybackLost() bool { return s.playLost.Load() }

// Discard drops everything the microphone has recorded so far. The capture thread confirms it
// when it next finds the device with nothing more to hand over; until then, and for the device's
// own period after, nothing it delivers is kept.
func (s *Stream) Discard() { s.discardBegin() }

// CaptureLost reports that the microphone stopped delivering: it was unplugged or disabled, or the
// audio service is gone. The call goes on without it; choosing another microphone is the user's
// decision.
func (s *Stream) CaptureLost() bool { return s.lost.Load() || s.stale() }

// Close stops both device threads.
func (s *Stream) Close() {
	s.closed.Store(true)
	s.wg.Wait()
}
