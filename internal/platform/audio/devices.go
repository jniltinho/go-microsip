//go:build linux || windows

package audio

import (
	"errors"
	"fmt"
	"sync"

	"github.com/jniltinho/go-microsip/internal/core/ports"
)

// Devices implements the core's ports.AudioDevices on the session's audio service. It connects when
// it is first used, and again after the service went away.
type Devices struct {
	mu     sync.Mutex
	client *Client
}

// connected returns the connection, making one when there is none.
func (d *Devices) connected() (*Client, error) {
	if d.client == nil {
		c, err := Connect()
		if err != nil {
			return nil, unavailable(err)
		}
		d.client = c
	}
	return d.client, nil
}

// unavailable reports a missing audio service in the core's terms, keeping its reason.
func unavailable(err error) error {
	if errors.Is(err, ErrNoService) {
		return fmt.Errorf("%w: %v", ports.ErrNoAudioService, err)
	}
	return err
}

// drop forgets a connection that failed, so the next use connects again.
func (d *Devices) drop() {
	if d.client != nil {
		d.client.Close()
		d.client = nil
	}
}

func (d *Devices) list(list func(*Client) ([]Device, error)) ([]ports.Device, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, err := d.connected()
	if err != nil {
		return nil, err
	}
	found, err := list(c)
	if err != nil {
		d.drop()
		return nil, unavailable(err)
	}
	out := make([]ports.Device, len(found))
	for i, dev := range found {
		out[i] = ports.Device(dev)
	}
	return out, nil
}

// Inputs lists the capture devices.
func (d *Devices) Inputs() ([]ports.Device, error) { return d.list((*Client).Inputs) }

// Outputs lists the playback devices.
func (d *Devices) Outputs() ([]ports.Device, error) { return d.list((*Client).Outputs) }

// Open starts one call's sound on the devices with these IDs; an empty ID is the default device.
// A device that does not exist is an error: no other one is opened in its place.
func (d *Devices) Open(input, output string) (ports.MediaIO, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	c, err := d.connected()
	if err != nil {
		return nil, err
	}
	s, err := c.Open(input, output)
	if err != nil {
		return nil, unavailable(err)
	}
	return s, nil
}

// Close ends the connection to the audio service.
func (d *Devices) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.drop()
}
