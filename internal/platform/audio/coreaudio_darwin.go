//go:build darwin && coreaudio

package audio

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// A prototype of the macOS backend (add-macos-universal, task 2.3), built only with the
// `coreaudio` tag: AudioToolbox's AudioQueue for capture and playback and CoreAudio's property
// calls for the device list, through purego, with no cgo. It exists to be measured; the adapter
// that ships is written with its tests in group 3.
//
// The playback queue is opened at 8 kHz mono 16-bit and AudioQueue converts to the device. The
// capture queue is opened at 48 kHz and brought down here (decimate.go): a capture queue hands
// over about 1024 frames at a time, which at 8 kHz is 128 ms in one burst.
// Callbacks arrive on threads AudioToolbox owns; each of the two callbacks is registered once
// and finds its stream by the instance number it was given as user data.

// queueBuffers is how many 20 ms buffers a queue is given.
const queueBuffers = 3

const (
	utf8          = 0x08000100       // kCFStringEncodingUTF8
	systemObject  = 1                // kAudioObjectSystemObject
	currentDevice = 0x61716364       // kAudioQueueProperty_CurrentDevice, 'aqcd'
	linearPCM     = 0x6c70636d       // kAudioFormatLinearPCM, 'lpcm'
	packedSigned  = 0x4 | 0x8        // signed integer, packed
	frameBytes    = FrameSamples * 2 // one 20 ms frame
	captureBytes  = frameBytes * decimation
)

var lib struct {
	once sync.Once
	err  error

	newInput    func(format *streamFormat, callback, user, runLoop, mode uintptr, flags uint32, queue *uintptr) int32
	newOutput   func(format *streamFormat, callback, user, runLoop, mode uintptr, flags uint32, queue *uintptr) int32
	allocate    func(queue uintptr, bytes uint32, buffer *uintptr) int32
	enqueue     func(queue, buffer uintptr, packets uint32, descriptions uintptr) int32
	start       func(queue, when uintptr) int32
	stop        func(queue uintptr, immediate bool) int32
	dispose     func(queue uintptr, immediate bool) int32
	setProperty func(queue uintptr, id uint32, data unsafe.Pointer, size uint32) int32
	dataSize    func(object uint32, address *propertyAddress, qualifierSize uint32, qualifier uintptr, size *uint32) int32
	data        func(object uint32, address *propertyAddress, qualifierSize uint32, qualifier uintptr, size *uint32, out unsafe.Pointer) int32
	newString   func(allocator uintptr, text string, encoding uint32) uintptr
	getString   func(text uintptr, buffer *byte, size int, encoding uint32) bool
	release     func(object uintptr)

	captured, wanted uintptr // the two callbacks, registered once: purego never frees one
}

// load binds the system's libraries the first time sound is used.
func load() error {
	lib.once.Do(func() {
		defer func() {
			if r := recover(); r != nil { // a symbol the system does not have
				lib.err = fmt.Errorf("%w: %v", ErrNoService, r)
			}
		}()
		open := func(path string) uintptr {
			handle, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL)
			if err != nil {
				panic(err)
			}
			return handle
		}
		toolbox := open("/System/Library/Frameworks/AudioToolbox.framework/AudioToolbox")
		core := open("/System/Library/Frameworks/CoreAudio.framework/CoreAudio")
		foundation := open("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation")
		purego.RegisterLibFunc(&lib.newInput, toolbox, "AudioQueueNewInput")
		purego.RegisterLibFunc(&lib.newOutput, toolbox, "AudioQueueNewOutput")
		purego.RegisterLibFunc(&lib.allocate, toolbox, "AudioQueueAllocateBuffer")
		purego.RegisterLibFunc(&lib.enqueue, toolbox, "AudioQueueEnqueueBuffer")
		purego.RegisterLibFunc(&lib.start, toolbox, "AudioQueueStart")
		purego.RegisterLibFunc(&lib.stop, toolbox, "AudioQueueStop")
		purego.RegisterLibFunc(&lib.dispose, toolbox, "AudioQueueDispose")
		purego.RegisterLibFunc(&lib.setProperty, toolbox, "AudioQueueSetProperty")
		purego.RegisterLibFunc(&lib.dataSize, core, "AudioObjectGetPropertyDataSize")
		purego.RegisterLibFunc(&lib.data, core, "AudioObjectGetPropertyData")
		purego.RegisterLibFunc(&lib.newString, foundation, "CFStringCreateWithCString")
		purego.RegisterLibFunc(&lib.getString, foundation, "CFStringGetCString")
		purego.RegisterLibFunc(&lib.release, foundation, "CFRelease")
		lib.captured = purego.NewCallback(capturedCallback)
		lib.wanted = purego.NewCallback(wantedCallback)
	})
	return lib.err
}

// live is what the callbacks find by instance number. An entry stays until its queue's disposal
// has returned, so what a callback uses is alive for as long as a callback can run.
var live struct {
	sync.RWMutex
	capture  map[uint64]*inputQueue
	playback map[uint64]*buffers
}

func init() {
	live.capture, live.playback = map[uint64]*inputQueue{}, map[uint64]*buffers{}
}

// capturedCallback is AudioQueueInputCallback: whatever the buffer brought, of any size, goes to
// the ring if its queue is the current one, and only then does the buffer go back to the queue.
func capturedCallback(user, queue, buffer, _, _, _ uintptr) uintptr {
	live.RLock()
	q := live.capture[uint64(user)]
	live.RUnlock()
	if q == nil {
		return 0
	}
	// One queue's callbacks come one at a time, so its conversion state needs no lock.
	filled := (*queueBuffer)(pointer(buffer))
	captured := unsafe.Slice((*int16)(filled.data), min(filled.size/2, uint32(len(q.converted))*decimation))
	if q.stream.publish(q.instance, q.converted[:q.down.convert(captured, q.converted[:])]) {
		lib.enqueue(queue, buffer, 0, 0)
	}
	return 0
}

// wantedCallback is AudioQueueOutputCallback: one frame out of the ring, or silence.
func wantedCallback(user, queue, buffer uintptr) uintptr {
	live.RLock()
	b := live.playback[uint64(user)]
	live.RUnlock()
	if b == nil {
		return 0
	}
	fill(b, queue, buffer)
	return 0
}

// fill hands the queue one frame: what the engine wrote, and silence for what it did not.
func fill(b *buffers, queue, buffer uintptr) {
	filled := (*queueBuffer)(pointer(buffer))
	p := unsafe.Slice((*int16)(filled.data), FrameSamples)
	if n := b.out.read(p); n < len(p) {
		clear(p[n:])
		b.silence.Add(uint64(len(p) - n))
	}
	filled.size = frameBytes
	lib.enqueue(queue, buffer, 0, 0)
}

var (
	queueFormat = streamFormat{
		sampleRate: SampleRate, formatID: linearPCM, formatFlags: packedSigned,
		bytesPerPacket: 2, framesPerPacket: 1, bytesPerFrame: 2, channelsPerFrame: 1, bitsPerChannel: 16,
	}
	captureFormat = streamFormat{
		sampleRate: captureRate, formatID: linearPCM, formatFlags: packedSigned,
		bytesPerPacket: 2, framesPerPacket: 1, bytesPerFrame: 2, channelsPerFrame: 1, bitsPerChannel: 16,
	}
)

// text reads a property that is a string and releases it.
func text(object uint32, selector string) string {
	address := propertyAddress{fourCC(selector), fourCC("glob"), 0}
	var (
		ref  uintptr
		size = uint32(unsafe.Sizeof(ref))
	)
	if lib.data(object, &address, 0, 0, &size, unsafe.Pointer(&ref)) != 0 || ref == 0 {
		return ""
	}
	defer lib.release(ref)
	var buffer [512]byte
	if !lib.getString(ref, &buffer[0], len(buffer), utf8) {
		return ""
	}
	for i, c := range buffer {
		if c == 0 {
			return string(buffer[:i])
		}
	}
	return ""
}

// devices lists the devices that have a stream in the scope ("inpt" or "outp"); defaultSelector
// names the system's default device of that scope.
func devices(scope, defaultSelector string) ([]Device, error) {
	if err := load(); err != nil {
		return nil, err
	}
	all := propertyAddress{fourCC("dev#"), fourCC("glob"), 0}
	var size uint32
	if status := lib.dataSize(systemObject, &all, 0, 0, &size); status != 0 {
		return nil, fmt.Errorf("the device list: status %d", status)
	}
	ids := make([]uint32, size/4)
	if len(ids) > 0 {
		if status := lib.data(systemObject, &all, 0, 0, &size, unsafe.Pointer(&ids[0])); status != 0 {
			return nil, fmt.Errorf("the device list: status %d", status)
		}
		ids = ids[:size/4]
	}
	var chosen uint32
	address, four := propertyAddress{fourCC(defaultSelector), fourCC("glob"), 0}, uint32(4)
	lib.data(systemObject, &address, 0, 0, &four, unsafe.Pointer(&chosen))
	var out []Device
	for _, id := range ids {
		streams := propertyAddress{fourCC("stm#"), fourCC(scope), 0}
		var bytes uint32
		if lib.dataSize(id, &streams, 0, 0, &bytes) != 0 || bytes == 0 {
			continue
		}
		if uid := text(id, "uid "); uid != "" {
			out = append(out, Device{ID: uid, Name: text(id, "lnam"), Default: id == chosen})
		}
	}
	return out, nil
}

// pin makes the queue use the device with this ID instead of the system's default one. A device
// that does not exist is an error: no other one is used in its place.
func pin(queue uintptr, id string, list []Device) error {
	if id == "" {
		return nil
	}
	known := false
	for _, device := range list {
		known = known || device.ID == id
	}
	if !known {
		return fmt.Errorf("device %q does not exist", id)
	}
	ref := lib.newString(0, id, utf8)
	if ref == 0 {
		return fmt.Errorf("device %q: its name cannot be given to the system", id)
	}
	defer lib.release(ref)
	if status := lib.setProperty(queue, currentDevice, unsafe.Pointer(&ref), uint32(unsafe.Sizeof(ref))); status != 0 {
		return fmt.Errorf("device %q: status %d", id, status)
	}
	return nil
}

// inputQueue is one started capture queue, with the state of its conversion to 8 kHz.
type inputQueue struct {
	ref       uintptr
	instance  uint64
	stream    *cutover
	down      decimator
	converted [2 * FrameSamples]int16 // room for the largest buffer a callback brings
}

// end stops the queue at once and disposes of it: immediate disposal is synchronous, and no
// callback of the queue comes after it. Only then is what its callbacks use forgotten.
func (q *inputQueue) end() {
	lib.stop(q.ref, true)
	lib.dispose(q.ref, true)
	live.Lock()
	delete(live.capture, q.instance)
	live.Unlock()
}

// Stream is one call's audio: capture queues, one after another, and one playback queue.
type Stream struct {
	*buffers
	capture *cutover
	play    *Output
}

// Output is a playback queue alone: what plays while a call has no audio of its own, such as its
// ring. No microphone is opened for it.
type Output struct {
	*buffers
	ref      uintptr
	instance uint64
}

// openOutput starts a playback queue on the device with this ID; empty is the default device.
func openOutput(id string) (*Output, error) {
	list, err := devices("outp", "dOut")
	if err != nil {
		return nil, err
	}
	o := &Output{buffers: newBuffers(), instance: instances.Add(1)}
	if status := lib.newOutput(&queueFormat, lib.wanted, uintptr(o.instance), 0, 0, 0, &o.ref); status != 0 {
		return nil, fmt.Errorf("open playback: status %d", status)
	}
	if err := pin(o.ref, id, list); err != nil {
		lib.dispose(o.ref, true)
		return nil, fmt.Errorf("output %w", err)
	}
	live.Lock()
	live.playback[o.instance] = o.buffers
	live.Unlock()
	for range queueBuffers {
		var buffer uintptr
		if status := lib.allocate(o.ref, frameBytes, &buffer); status != 0 {
			o.Close()
			return nil, fmt.Errorf("open playback: a buffer: status %d", status)
		}
		fill(o.buffers, o.ref, buffer)
	}
	if status := lib.start(o.ref, 0); status != 0 {
		o.Close()
		return nil, fmt.Errorf("open playback: start: status %d", status)
	}
	return o, nil
}

// Close stops the queue.
func (o *Output) Close() {
	lib.stop(o.ref, true)
	lib.dispose(o.ref, true)
	live.Lock()
	delete(live.playback, o.instance)
	live.Unlock()
}

// open starts one call's sound. Playback starts before it returns; capture is opened behind it,
// because macOS holds the start of a capture queue while it asks the user for the microphone.
func open(input, output string) (*Stream, error) {
	inputs, err := devices("inpt", "dIn ")
	if err != nil {
		return nil, err
	}
	if input != "" {
		known := false
		for _, device := range inputs {
			known = known || device.ID == input
		}
		if !known {
			return nil, fmt.Errorf("input device %q does not exist", input)
		}
	}
	play, err := openOutput(output)
	if err != nil {
		return nil, err
	}
	s := &Stream{buffers: play.buffers, play: play}
	var c *cutover
	ready := make(chan struct{})
	c = newCutover(s.buffers, func(instance uint64) (captureQueue, error) {
		<-ready // c is set
		var ref uintptr
		if status := lib.newInput(&captureFormat, lib.captured, uintptr(instance), 0, 0, 0, &ref); status != 0 {
			return nil, fmt.Errorf("open capture: status %d", status)
		}
		if err := pin(ref, input, inputs); err != nil {
			lib.dispose(ref, true)
			return nil, fmt.Errorf("input %w", err)
		}
		q := &inputQueue{ref: ref, instance: instance, stream: c}
		live.Lock()
		live.capture[instance] = q
		live.Unlock()
		for range queueBuffers {
			var buffer uintptr
			status := lib.allocate(ref, captureBytes, &buffer)
			if status == 0 {
				status = lib.enqueue(ref, buffer, 0, 0)
			}
			if status != 0 {
				q.end()
				return nil, fmt.Errorf("open capture: a buffer: status %d", status)
			}
		}
		if status := lib.start(ref, 0); status != 0 {
			q.end()
			return nil, fmt.Errorf("open capture: start: status %d", status)
		}
		return q, nil
	})
	s.capture = c
	close(ready)
	return s, nil
}

// Discard drops everything the microphone has recorded so far: the capture queue that recorded
// it is retired and a new one is opened in its place.
func (s *Stream) Discard() { s.capture.discard() }

// CaptureLost reports that the microphone stopped delivering, or that a capture queue could not
// be opened.
func (s *Stream) CaptureLost() bool { return s.capture.lost.Load() || s.stale() }

// Close stops both sides. A capture queue still being opened is disposed of when its opening
// returns.
func (s *Stream) Close() {
	s.capture.close()
	s.play.Close()
}

// Devices implements the core's ports.AudioDevices on CoreAudio.
type Devices struct{}

func listed(list []Device, err error) ([]ports.Device, error) {
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ports.ErrNoAudioService, err)
	}
	out := make([]ports.Device, len(list))
	for i, device := range list {
		out[i] = ports.Device(device)
	}
	return out, nil
}

// Inputs lists the capture devices.
func (*Devices) Inputs() ([]ports.Device, error) { return listed(devices("inpt", "dIn ")) }

// Outputs lists the playback devices.
func (*Devices) Outputs() ([]ports.Device, error) { return listed(devices("outp", "dOut")) }

// Open starts one call's sound on the devices with these IDs; an empty ID is the default device.
func (*Devices) Open(input, output string) (ports.MediaIO, error) {
	s, err := open(input, output)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// OpenOutput starts playback alone on the device with this ID. No microphone is opened.
func (*Devices) OpenOutput(output string) (ports.Player, error) {
	o, err := openOutput(output)
	if err != nil {
		return nil, err
	}
	return o, nil
}

// Close does nothing: the prototype keeps no connection.
func (*Devices) Close() {}
