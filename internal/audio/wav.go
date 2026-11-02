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

// JoinWAV joins verified mono PCM recordings without changing their samples.
func JoinWAV(a, b []byte) ([]byte, error) {
	da, ea := WAVDuration(a)
	db, eb := WAVDuration(b)
	if ea != nil || eb != nil || da+db > 12*time.Second {
		return nil, errors.New("audio: invalid overlap window")
	}
	pcm := func(wav []byte) []byte {
		for i := 12; i+8 <= len(wav); {
			size := int(binary.LittleEndian.Uint32(wav[i+4 : i+8]))
			start := i + 8
			if string(wav[i:i+4]) == "data" {
				return wav[start : start+size]
			}
			i = start + size + (size & 1)
		}
		return nil
	}
	left, right := pcm(a), pcm(b)
	out := make([]byte, 44+len(left)+len(right))
	copy(out, "RIFF")
	binary.LittleEndian.PutUint32(out[4:], uint32(len(out)-8))
	copy(out[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(out[16:], 16)
	binary.LittleEndian.PutUint16(out[20:], 1)
	binary.LittleEndian.PutUint16(out[22:], 1)
	binary.LittleEndian.PutUint32(out[24:], 16000)
	binary.LittleEndian.PutUint32(out[28:], 32000)
	binary.LittleEndian.PutUint16(out[32:], 2)
	binary.LittleEndian.PutUint16(out[34:], 16)
	copy(out[36:], "data")
	binary.LittleEndian.PutUint32(out[40:], uint32(len(left)+len(right)))
	copy(out[44:], left)
	copy(out[44+len(left):], right)
	return out, nil
}
