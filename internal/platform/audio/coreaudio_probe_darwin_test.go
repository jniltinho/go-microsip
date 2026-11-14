//go:build darwin

package audio

// A probe, not the adapter (add-macos-universal, task 2.3): does a callback that AudioToolbox
// makes on a thread of its own enter Go without cgo, on time, while the collector runs, and what
// does the path cost? It plays near-silence on the default output for a few seconds and lists the
// system's sound devices. It needs no permission and no desktop session: run it on a Mac from the
// cross-built test binary. The capture probe below it is asked for by name: it needs the
// microphone, which macOS grants to the program that started the test, not to the test.

import (
	"fmt"
	"math"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

const probeBuffers = 3

var probe struct {
	enqueue   func(queue, buffer uintptr, packets uint32, descriptions uintptr) int32
	calls     atomic.Int64
	last      atomic.Int64 // when the previous callback came, in nanoseconds
	longest   atomic.Int64 // the longest wait between two callbacks
	slowest   atomic.Int64 // the longest a callback itself took
	stopped   atomic.Bool
	startedAt time.Time
}

// render is the output callback: one 20 ms frame of a very quiet tone, handed back to the queue.
// It only copies and counts.
func render(_, queue, buffer uintptr) uintptr {
	began := time.Since(probe.startedAt).Nanoseconds()
	if previous := probe.last.Swap(began); previous != 0 {
		if wait := began - previous; wait > probe.longest.Load() {
			probe.longest.Store(wait)
		}
	}
	if probe.stopped.Load() {
		return 0
	}
	filled := (*queueBuffer)(pointer(buffer))
	samples := unsafe.Slice((*int16)(filled.data), FrameSamples)
	for i := range samples {
		samples[i] = int16((i%16 - 8) * 4)
	}
	filled.size = FrameSamples * 2
	probe.enqueue(queue, buffer, 0, 0)
	probe.calls.Add(1)
	if took := time.Since(probe.startedAt).Nanoseconds() - began; took > probe.slowest.Load() {
		probe.slowest.Store(took)
	}
	return 0
}

func TestProbeCoreAudioFromGoWithoutCgo(t *testing.T) {
	toolbox, err := purego.Dlopen("/System/Library/Frameworks/AudioToolbox.framework/AudioToolbox", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		t.Fatal(err)
	}
	var (
		newOutput func(format *streamFormat, callback, user, runLoop, mode uintptr, flags uint32, queue *uintptr) int32
		allocate  func(queue uintptr, bytes uint32, buffer *uintptr) int32
		start     func(queue, when uintptr) int32
		stop      func(queue uintptr, immediate bool) int32
		dispose   func(queue uintptr, immediate bool) int32
	)
	purego.RegisterLibFunc(&newOutput, toolbox, "AudioQueueNewOutput")
	purego.RegisterLibFunc(&allocate, toolbox, "AudioQueueAllocateBuffer")
	purego.RegisterLibFunc(&probe.enqueue, toolbox, "AudioQueueEnqueueBuffer")
	purego.RegisterLibFunc(&start, toolbox, "AudioQueueStart")
	purego.RegisterLibFunc(&stop, toolbox, "AudioQueueStop")
	purego.RegisterLibFunc(&dispose, toolbox, "AudioQueueDispose")

	t.Run("devices", func(t *testing.T) {
		core, err := purego.Dlopen("/System/Library/Frameworks/CoreAudio.framework/CoreAudio", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			t.Fatal(err)
		}
		var (
			dataSize func(object uint32, address *propertyAddress, qualifierSize uint32, qualifier uintptr, size *uint32) int32
			data     func(object uint32, address *propertyAddress, qualifierSize uint32, qualifier uintptr, size *uint32, out unsafe.Pointer) int32
		)
		purego.RegisterLibFunc(&dataSize, core, "AudioObjectGetPropertyDataSize")
		purego.RegisterLibFunc(&data, core, "AudioObjectGetPropertyData")
		const system = 1 // kAudioObjectSystemObject
		all := propertyAddress{fourCC("dev#"), fourCC("glob"), 0}
		var size uint32
		if status := dataSize(system, &all, 0, 0, &size); status != 0 {
			t.Fatalf("the size of the device list: status %d", status)
		}
		devices := make([]uint32, size/4)
		if len(devices) > 0 {
			if status := data(system, &all, 0, 0, &size, unsafe.Pointer(&devices[0])); status != 0 {
				t.Fatalf("the device list: status %d", status)
			}
		}
		inputs, outputs := 0, 0
		for _, device := range devices {
			for scope, count := range map[string]*int{"inpt": &inputs, "outp": &outputs} {
				streams := propertyAddress{fourCC("stm#"), fourCC(scope), 0}
				var bytes uint32
				if dataSize(device, &streams, 0, 0, &bytes) == 0 && bytes > 0 {
					*count++
				}
			}
		}
		t.Logf("%d devices: %d with an input, %d with an output", len(devices), inputs, outputs)
		// What each device runs at: its rate, and how many frames it hands over at a time.
		for i, device := range devices {
			var (
				rate   float64
				frames uint32
				limits [2]float64
			)
			eight, four, sixteen := uint32(8), uint32(4), uint32(16)
			data(device, &propertyAddress{fourCC("nsrt"), fourCC("glob"), 0}, 0, 0, &eight, unsafe.Pointer(&rate))
			data(device, &propertyAddress{fourCC("fsiz"), fourCC("glob"), 0}, 0, 0, &four, unsafe.Pointer(&frames))
			data(device, &propertyAddress{fourCC("fsz#"), fourCC("glob"), 0}, 0, 0, &sixteen, unsafe.Pointer(&limits))
			var in, out uint32
			dataSize(device, &propertyAddress{fourCC("stm#"), fourCC("inpt"), 0}, 0, 0, &in)
			dataSize(device, &propertyAddress{fourCC("stm#"), fourCC("outp"), 0}, 0, 0, &out)
			t.Logf("device %d (input %v, output %v): %.0f Hz, %d frames at a time (%.0f ms), may be %.0f to %.0f",
				i, in > 0, out > 0, rate, frames, float64(frames)/rate*1000, limits[0], limits[1])
		}
		if outputs == 0 {
			t.Skip("no output device: the playback probe cannot run here")
		}
	})

	format := streamFormat{
		sampleRate: SampleRate, formatID: fourCC("lpcm"), formatFlags: 0x4 | 0x8, // signed integer, packed
		bytesPerPacket: 2, framesPerPacket: 1, bytesPerFrame: 2, channelsPerFrame: 1, bitsPerChannel: 16,
	}
	callback := purego.NewCallback(render)
	var queue uintptr
	if status := newOutput(&format, callback, 0, 0, 0, 0, &queue); status != 0 {
		t.Fatalf("an 8 kHz mono output queue: status %d", status)
	}
	defer dispose(queue, true)
	probe.startedAt = time.Now()
	for range probeBuffers {
		var buffer uintptr
		if status := allocate(queue, FrameSamples*2, &buffer); status != 0 {
			t.Fatalf("a buffer: status %d", status)
		}
		render(0, queue, buffer)
	}
	probe.calls.Store(0)
	probe.last.Store(0)
	probe.longest.Store(0)
	// Without a desktop session no queue starts, the system's own player's included (afplay
	// fails with this status over SSH on a Mac where nobody is logged in).
	const cannotStart = -66681 // kAudioQueueErr_CannotStart
	if status := start(queue, 0); status == cannotStart {
		t.Skip("the system starts no audio queue in this session: run the probe in a desktop session")
	} else if status != 0 {
		t.Fatalf("starting the queue: status %d", status)
	}

	// Quiet: what the callback path itself allocates.
	var before, after runtime.MemStats
	time.Sleep(500 * time.Millisecond)
	runtime.ReadMemStats(&before)
	quietFrom := probe.calls.Load()
	time.Sleep(2 * time.Second)
	quiet := probe.calls.Load() - quietFrom
	runtime.ReadMemStats(&after)
	t.Logf("quiet, 2 s: %d callbacks, %d allocations in the whole process, longest wait %v, slowest callback %v",
		quiet, after.Mallocs-before.Mallocs, time.Duration(probe.longest.Load()), time.Duration(probe.slowest.Load()))

	// Busy: goroutines that allocate, and the collector forced again and again.
	probe.longest.Store(0)
	probe.slowest.Store(0)
	busyFrom := probe.calls.Load()
	done := make(chan struct{})
	for range runtime.NumCPU() {
		go func() {
			var keep [][]byte
			for {
				select {
				case <-done:
					return
				default:
					keep = append(keep, make([]byte, 64<<10))
					if len(keep) > 256 {
						keep = nil
					}
				}
			}
		}()
	}
	collections := 0
	for until := time.Now().Add(5 * time.Second); time.Now().Before(until); collections++ {
		runtime.GC()
	}
	close(done)
	busy := probe.calls.Load() - busyFrom
	t.Logf("busy, 5 s, %d forced collections: %d callbacks, longest wait %v, slowest callback %v",
		collections, busy, time.Duration(probe.longest.Load()), time.Duration(probe.slowest.Load()))
	probe.stopped.Store(true)
	if status := stop(queue, true); status != 0 {
		t.Errorf("stopping the queue: status %d", status)
	}

	// 50 frames a second: nine in ten of them at least, and no wait of three frames.
	if quiet < 90 || busy < 225 {
		t.Errorf("too few callbacks: %d in 2 quiet seconds, %d in 5 busy ones", quiet, busy)
	}
	if wait := time.Duration(probe.longest.Load()); wait > 60*time.Millisecond {
		t.Errorf("a wait of %v between two callbacks under load", wait)
	}
}

var capture struct {
	enqueue   func(queue, buffer uintptr, packets uint32, descriptions uintptr) int32
	samples   []int16      // what was captured, allocated before the queue starts
	taken     atomic.Int64 // samples copied so far
	played    atomic.Int64 // samples handed back to the output so far
	calls     atomic.Int64
	smallest  atomic.Int64 // the fewest bytes one callback brought
	largest   atomic.Int64 // the most
	last      atomic.Int64
	longest   atomic.Int64
	arrivals  []int64 // when each callback came, in nanoseconds since the start
	startedAt time.Time
}

// captured is the input callback: it copies what the buffer brought and hands the buffer back.
func captured(_, queue, buffer, _, _, _ uintptr) uintptr {
	now := time.Since(capture.startedAt).Nanoseconds()
	if previous := capture.last.Swap(now); previous != 0 {
		if wait := now - previous; wait > capture.longest.Load() {
			capture.longest.Store(wait)
		}
	}
	filled := (*queueBuffer)(pointer(buffer))
	bytes := int64(filled.size)
	call := capture.calls.Add(1)
	if call <= int64(len(capture.arrivals)) {
		capture.arrivals[call-1] = now
	}
	if call == 1 || bytes < capture.smallest.Load() {
		capture.smallest.Store(bytes)
	}
	if bytes > capture.largest.Load() {
		capture.largest.Store(bytes)
	}
	at, count := capture.taken.Load(), bytes/2
	if room := int64(len(capture.samples)) - at; count > room {
		count = room
	}
	if count > 0 {
		copy(capture.samples[at:at+count], unsafe.Slice((*int16)(filled.data), count))
		capture.taken.Store(at + count)
	}
	capture.enqueue(queue, buffer, 0, 0)
	return 0
}

// replay is the output callback of the second half: the captured samples, one frame at a time.
func replay(_, queue, buffer uintptr) uintptr {
	filled := (*queueBuffer)(pointer(buffer))
	samples := unsafe.Slice((*int16)(filled.data), FrameSamples)
	at := capture.played.Add(FrameSamples) - FrameSamples
	for i := range samples {
		samples[i] = 0
		if j := at + int64(i); j < capture.taken.Load() {
			samples[i] = capture.samples[j]
		}
	}
	filled.size = FrameSamples * 2
	capture.enqueue(queue, buffer, 0, 0)
	return 0
}

// TestProbeCaptureFromGoWithoutCgo records four seconds from the default input at 8 kHz mono,
// whatever rate the device runs at, and plays them back on the default output.
func TestProbeCaptureFromGoWithoutCgo(t *testing.T) {
	if os.Getenv("GO_MICROSIP_PROBE_CAPTURE") == "" {
		t.Skip("set GO_MICROSIP_PROBE_CAPTURE=1: capturing asks macOS for the microphone")
	}
	toolbox, err := purego.Dlopen("/System/Library/Frameworks/AudioToolbox.framework/AudioToolbox", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		t.Fatal(err)
	}
	var (
		newInput  func(format *streamFormat, callback, user, runLoop, mode uintptr, flags uint32, queue *uintptr) int32
		newOutput func(format *streamFormat, callback, user, runLoop, mode uintptr, flags uint32, queue *uintptr) int32
		allocate  func(queue uintptr, bytes uint32, buffer *uintptr) int32
		start     func(queue, when uintptr) int32
		stop      func(queue uintptr, immediate bool) int32
		dispose   func(queue uintptr, immediate bool) int32
	)
	purego.RegisterLibFunc(&newInput, toolbox, "AudioQueueNewInput")
	purego.RegisterLibFunc(&newOutput, toolbox, "AudioQueueNewOutput")
	purego.RegisterLibFunc(&allocate, toolbox, "AudioQueueAllocateBuffer")
	purego.RegisterLibFunc(&capture.enqueue, toolbox, "AudioQueueEnqueueBuffer")
	purego.RegisterLibFunc(&start, toolbox, "AudioQueueStart")
	purego.RegisterLibFunc(&stop, toolbox, "AudioQueueStop")
	purego.RegisterLibFunc(&dispose, toolbox, "AudioQueueDispose")

	format := streamFormat{
		sampleRate: SampleRate, formatID: fourCC("lpcm"), formatFlags: 0x4 | 0x8, // signed integer, packed
		bytesPerPacket: 2, framesPerPacket: 1, bytesPerFrame: 2, channelsPerFrame: 1, bitsPerChannel: 16,
	}
	// GO_MICROSIP_PROBE_FRAMES asks the default input for this many frames at a time, for as long
	// as this process lives: does the queue then call back more often?
	if frames, err := strconv.Atoi(os.Getenv("GO_MICROSIP_PROBE_FRAMES")); err == nil && frames > 0 {
		core, err := purego.Dlopen("/System/Library/Frameworks/CoreAudio.framework/CoreAudio", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			t.Fatal(err)
		}
		var (
			get func(object uint32, address *propertyAddress, qualifierSize uint32, qualifier uintptr, size *uint32, out unsafe.Pointer) int32
			set func(object uint32, address *propertyAddress, qualifierSize uint32, qualifier uintptr, size uint32, in unsafe.Pointer) int32
		)
		purego.RegisterLibFunc(&get, core, "AudioObjectGetPropertyData")
		purego.RegisterLibFunc(&set, core, "AudioObjectSetPropertyData")
		var device uint32
		four, wanted := uint32(4), uint32(frames)
		get(1, &propertyAddress{fourCC("dIn "), fourCC("glob"), 0}, 0, 0, &four, unsafe.Pointer(&device))
		status := set(device, &propertyAddress{fourCC("fsiz"), fourCC("glob"), 0}, 0, 0, 4, unsafe.Pointer(&wanted))
		var now uint32
		get(device, &propertyAddress{fourCC("fsiz"), fourCC("glob"), 0}, 0, 0, &four, unsafe.Pointer(&now))
		t.Logf("asked the input for %d frames at a time: status %d, it now hands over %d", frames, status, now)
	}
	// GO_MICROSIP_PROBE_RATE opens the input queue at this rate instead of 8 kHz: does the
	// queue hand over a fixed number of frames at a time, whatever they last?
	rate := SampleRate
	if asked, err := strconv.Atoi(os.Getenv("GO_MICROSIP_PROBE_RATE")); err == nil && asked > 0 {
		rate = asked
		format.sampleRate = float64(rate)
	}
	const seconds = 4
	capture.samples = make([]int16, rate*seconds)
	capture.arrivals = make([]int64, seconds*100)
	var input uintptr
	if status := newInput(&format, purego.NewCallback(captured), 0, 0, 0, 0, &input); status != 0 {
		t.Fatalf("an 8 kHz mono input queue: status %d", status)
	}
	for range probeBuffers {
		var buffer uintptr
		if status := allocate(input, uint32(rate/50*2), &buffer); status != 0 {
			t.Fatalf("a buffer: status %d", status)
		}
		if status := capture.enqueue(input, buffer, 0, 0); status != 0 {
			t.Fatalf("handing a buffer to the input queue: status %d", status)
		}
	}
	capture.startedAt = time.Now()
	if status := start(input, 0); status != 0 {
		dispose(input, true)
		t.Fatalf("starting the input queue: status %d", status)
	}
	time.Sleep(seconds*time.Second + 200*time.Millisecond)
	if status := stop(input, true); status != 0 {
		t.Errorf("stopping the input queue: status %d", status)
	}
	dispose(input, true)

	taken := capture.taken.Load()
	peak, squares := 0, 0.0
	for _, sample := range capture.samples[:taken] {
		if magnitude := int(sample); magnitude > peak {
			peak = magnitude
		} else if -magnitude > peak {
			peak = -magnitude
		}
		squares += float64(sample) * float64(sample)
	}
	rms := 0.0
	if taken > 0 {
		rms = math.Sqrt(squares / float64(taken))
	}
	t.Logf("captured, %d s: %d callbacks of %d to %d bytes, %d samples, longest wait %v, peak %d, rms %.0f",
		seconds, capture.calls.Load(), capture.smallest.Load(), capture.largest.Load(), taken,
		time.Duration(capture.longest.Load()), peak, rms)
	// How the callbacks were spread: a steady device brings one every 20 ms.
	arrivals := capture.arrivals[:min(capture.calls.Load(), int64(len(capture.arrivals)))]
	var together, steady, late, gaps int
	var gapsAt []string
	for i := 1; i < len(arrivals); i++ {
		switch wait := time.Duration(arrivals[i] - arrivals[i-1]); {
		case wait < 5*time.Millisecond:
			together++
		case wait < 30*time.Millisecond:
			steady++
		case wait < 100*time.Millisecond:
			late++
		default:
			gaps++
			if len(gapsAt) < 12 {
				gapsAt = append(gapsAt, fmt.Sprintf("%v at %v", wait.Round(time.Millisecond), time.Duration(arrivals[i]).Round(10*time.Millisecond)))
			}
		}
	}
	if len(arrivals) > 0 {
		t.Logf("the first callback came %v after the start; waits between callbacks: %d under 5 ms, %d of 5 to 30 ms, %d of 30 to 100 ms, %d longer %v",
			time.Duration(arrivals[0]).Round(time.Millisecond), together, steady, late, gaps, gapsAt)
	}
	if taken < int64(rate*seconds*9/10) {
		t.Errorf("%d samples in %d seconds: fewer than nine in ten", taken, seconds)
	}
	if peak == 0 {
		t.Fatalf("only zeros: macOS hands silence to a program that was not given the microphone")
	}

	if rate != SampleRate {
		return // the playback below is written for 8 kHz
	}
	var output uintptr
	if status := newOutput(&format, purego.NewCallback(replay), 0, 0, 0, 0, &output); status != 0 {
		t.Fatalf("an 8 kHz mono output queue: status %d", status)
	}
	defer dispose(output, true)
	for range probeBuffers {
		var buffer uintptr
		if status := allocate(output, FrameSamples*2, &buffer); status != 0 {
			t.Fatalf("a buffer: status %d", status)
		}
		replay(0, output, buffer)
	}
	if status := start(output, 0); status != 0 {
		t.Fatalf("starting the output queue: status %d", status)
	}
	time.Sleep(seconds*time.Second + 300*time.Millisecond)
	stop(output, true)
	t.Logf("played back: %d of %d samples", min(capture.played.Load(), taken), taken)
}
