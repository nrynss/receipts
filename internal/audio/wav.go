package audio

import (
	"encoding/binary"
	"errors"
	"time"
)

// wavDuration accepts the capture pipeline's mono 16 kHz signed PCM WAV only.
func WAVDuration(b []byte) (time.Duration, error) {
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WAVE" || uint64(binary.LittleEndian.Uint32(b[4:8]))+8 != uint64(len(b)) {
		return 0, errors.New("audio: invalid WAV container")
	}
	var format, data bool
	var samples uint32
	for i := 12; i+8 <= len(b); {
		size := int(binary.LittleEndian.Uint32(b[i+4 : i+8]))
		start := i + 8
		if size > len(b)-start {
			return 0, errors.New("audio: invalid WAV chunk")
		}
		switch string(b[i : i+4]) {
		case "fmt ":
			if format || size < 16 || binary.LittleEndian.Uint16(b[start:]) != 1 || binary.LittleEndian.Uint16(b[start+2:]) != 1 || binary.LittleEndian.Uint32(b[start+4:]) != 16000 || binary.LittleEndian.Uint32(b[start+8:]) != 32000 || binary.LittleEndian.Uint16(b[start+12:]) != 2 || binary.LittleEndian.Uint16(b[start+14:]) != 16 {
				return 0, errors.New("audio: unsupported WAV format")
			}
			format = true
		case "data":
			if data || size == 0 || size%2 != 0 {
				return 0, errors.New("audio: invalid WAV data")
			}
			data = true
			samples = uint32(size / 2)
		}
		i = start + size + (size & 1)
		if i > len(b) {
			return 0, errors.New("audio: invalid WAV padding")
		}
	}
	if !format || !data {
		return 0, errors.New("audio: incomplete WAV")
	}
	return time.Duration(samples) * time.Second / 16000, nil
}
