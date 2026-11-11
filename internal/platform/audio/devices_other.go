//go:build !linux && !windows

package audio

import "github.com/jniltinho/go-microsip/internal/core/ports"

// Devices is the sound of a system that has no backend yet: every use reports that no audio service
// is reachable.
type Devices struct{}

// Inputs reports that no audio service is reachable.
func (*Devices) Inputs() ([]ports.Device, error) { return nil, ports.ErrNoAudioService }

// Outputs reports that no audio service is reachable.
func (*Devices) Outputs() ([]ports.Device, error) { return nil, ports.ErrNoAudioService }

// Open reports that no audio service is reachable.
func (*Devices) Open(_, _ string) (ports.MediaIO, error) { return nil, ports.ErrNoAudioService }

// Close does nothing.
func (*Devices) Close() {}
