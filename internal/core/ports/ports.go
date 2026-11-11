// Package ports holds the interfaces through which the core reaches the operating system. The core
// owns them; internal/platform implements them per OS, and package memory implements them for
// tests. A capability a system lacks is reported as unavailable, never simulated.
package ports

import (
	"errors"
	"os"
)

// FrameSamples is one 20 ms frame of 8 kHz mono audio.
const FrameSamples = 160

// ErrNoAudioService means the session offers no audio service the adapter can reach.
var ErrNoAudioService = errors.New("no audio service is reachable")

// Device is one sound input or output.
type Device struct {
	// ID is stable for the device and is what settings store.
	ID string
	// Name is what the user sees.
	Name string
	// Default marks the device used when none is chosen.
	Default bool
}

// MediaIO is the sound of one call. The engine calls Capture and Play once per 20 ms media tick;
// neither may block or allocate.
type MediaIO interface {
	// Capture fills frame with the next 20 ms from the microphone and reports whether it had one.
	Capture(frame *[FrameSamples]int16) bool
	// Discard drops everything the microphone has recorded and not handed out yet. The engine
	// calls it when a hold or a mute ends: what was recorded meanwhile must never be sent. Like
	// Capture, it is called from the engine's side only and must not block.
	Discard()
	// Play queues 20 ms for the speaker.
	Play(frame *[FrameSamples]int16)
	// CaptureLost reports that the microphone stopped delivering (unplugged, or the service is
	// gone). The call goes on; another microphone is the user's choice.
	CaptureLost() bool
	// Close releases the devices.
	Close()
}

// AudioDevices is the session's sound devices.
type AudioDevices interface {
	// Inputs lists the capture devices.
	Inputs() ([]Device, error)
	// Outputs lists the playback devices.
	Outputs() ([]Device, error)
	// Open starts capture and playback on the devices with these IDs; an empty ID is the default
	// device. It fails with ErrNoAudioService when the session has no audio service.
	Open(input, output string) (MediaIO, error)
}

// FileAccess tells the configuration who may touch a file, so it can refuse a settings file that
// another user could rewrite and a secret that anyone else could read.
type FileAccess interface {
	// CurrentUser is the numeric user the process runs as.
	CurrentUser() uint32
	// Inspect returns the owner and the permission bits, as Unix mode bits, of a file.
	Inspect(info os.FileInfo) (owner, mode uint32)
}

// ErrLocked means another process holds the lock.
var ErrLocked = errors.New("the lock is held by another process")

// Locker takes the advisory lock by which an application keeps one instance per user. The lock is
// released by the returned function, and by the system when the process dies.
type Locker interface {
	// Lock takes the lock file at path, creating it and its private directory when missing. It
	// does not wait: a lock held elsewhere is ErrLocked.
	Lock(path string) (unlock func(), err error)
}

// ErrNoSecretService means the session offers no keyring this adapter can use.
var ErrNoSecretService = errors.New("no secret service is reachable")

// Secrets is the user's keyring: passwords are stored there and nowhere else, under a reference
// that is not secret and is what the database keeps.
type Secrets interface {
	// Get returns the password stored under ref; ok is false when there is none.
	Get(ref string) (password string, ok bool, err error)
	// Set stores or replaces the password under ref.
	Set(ref, password string) error
	// Delete removes the entry; removing one that does not exist is not an error.
	Delete(ref string) error
}
