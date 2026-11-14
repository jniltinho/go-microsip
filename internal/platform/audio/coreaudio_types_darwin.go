//go:build darwin

package audio

import "unsafe"

// The layouts and constants of CoreAudio and AudioToolbox that the probe and the adapter share.

// streamFormat is AudioStreamBasicDescription.
type streamFormat struct {
	sampleRate       float64
	formatID         uint32
	formatFlags      uint32
	bytesPerPacket   uint32
	framesPerPacket  uint32
	bytesPerFrame    uint32
	channelsPerFrame uint32
	bitsPerChannel   uint32
	reserved         uint32
}

// queueBuffer is the start of AudioQueueBuffer.
type queueBuffer struct {
	capacity uint32
	data     unsafe.Pointer
	size     uint32
}

// propertyAddress is AudioObjectPropertyAddress.
type propertyAddress struct{ selector, scope, element uint32 }

func fourCC(code string) uint32 {
	return uint32(code[0])<<24 | uint32(code[1])<<16 | uint32(code[2])<<8 | uint32(code[3])
}

// pointer turns an address the system handed over into a pointer.
func pointer(address uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&address)) }
