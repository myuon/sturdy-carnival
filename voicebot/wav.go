package voicebot

import (
	"encoding/binary"
	"io"
)

type WavHeader struct {
	SampleRate    uint32
	Channels      uint16
	BitsPerSample uint16
	DataSize      uint32
}

func (w *WavHeader) DurationSeconds() float64 {
	bytesPerSample := w.BitsPerSample / 8
	bytesPerSecond := uint32(w.Channels) * w.SampleRate * uint32(bytesPerSample)
	return float64(w.DataSize) / float64(bytesPerSecond)
}

func ReadWavHeader(r io.Reader) (*WavHeader, error) {
	var header [44]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}

	return &WavHeader{
		SampleRate:    binary.LittleEndian.Uint32(header[24:28]),
		Channels:      binary.LittleEndian.Uint16(header[22:24]),
		BitsPerSample: binary.LittleEndian.Uint16(header[34:36]),
		DataSize:      binary.LittleEndian.Uint32(header[40:44]),
	}, nil
}
