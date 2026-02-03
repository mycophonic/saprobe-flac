/*
   Copyright Mycophonic.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package flac

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	goflac "github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
	"github.com/mewkiz/flac/meta"
)

var errPCMLengthMismatch = errors.New("pcm length is not a multiple of frame size")

const (
	defaultBlockSize = 4096
	sign24Bit        = 0x800000
	mask24Bit        = 0xFFFFFF
)

// Encode writes interleaved little-endian signed PCM bytes as a FLAC stream to writer.
// It is the inverse of Decode.
func Encode(writer io.Writer, pcm []byte, format PCMFormat) error {
	nChannels := int(format.Channels) //nolint:gosec // Channels is 1-8, fits int.
	bytesPerSample := format.BitDepth.BytesPerSample()
	frameSize := nChannels * bytesPerSample

	if len(pcm)%frameSize != 0 {
		return fmt.Errorf("%w: pcm=%d, frame=%d", errPCMLengthMismatch, len(pcm), frameSize)
	}

	totalSamples := len(pcm) / frameSize

	info := &meta.StreamInfo{
		BlockSizeMin:  defaultBlockSize,
		BlockSizeMax:  defaultBlockSize,
		SampleRate:    uint32(format.SampleRate), //nolint:gosec // SampleRate is always positive and fits uint32.
		NChannels:     uint8(nChannels),          //nolint:gosec // Channels is 1-8, fits uint8.
		BitsPerSample: uint8(format.BitDepth),    //nolint:gosec // BitDepth is 4-32, fits uint8.
		NSamples:      uint64(totalSamples),      //nolint:gosec // totalSamples is always positive.
	}

	enc, err := goflac.NewEncoder(writer, info)
	if err != nil {
		return fmt.Errorf("creating encoder: %w", err)
	}

	// Pre-allocate per-channel buffers at max block size; reused across frames.
	channels := make([][]int32, nChannels)
	for ch := range channels {
		channels[ch] = make([]int32, defaultBlockSize)
	}

	remaining := totalSamples
	offset := 0

	for remaining > 0 {
		blockSamples := min(remaining, defaultBlockSize)

		deinterleave(channels, pcm, offset, blockSamples, nChannels, format.BitDepth)
		offset += blockSamples * frameSize
		remaining -= blockSamples

		f := buildFrame(channels, blockSamples, format)

		if err := enc.WriteFrame(f); err != nil {
			return fmt.Errorf("writing frame: %w", err)
		}
	}

	if err := enc.Close(); err != nil {
		return fmt.Errorf("closing encoder: %w", err)
	}

	return nil
}

// deinterleave reads interleaved little-endian signed PCM bytes into pre-allocated
// per-channel int32 slices. It is the inverse of interleave in decode.go.
//
//nolint:varnamelen // Loop variables i, ch, s are idiomatic.
func deinterleave(channels [][]int32, pcm []byte, offset, blockSize, nChannels int, depth BitDepth) {
	// Reslice to exact block size (channels were allocated at max block size).
	for ch := range channels {
		channels[ch] = channels[ch][:blockSize]
	}

	pos := offset

	switch depth {
	case Depth4, Depth8:
		for i := range blockSize {
			for ch := range nChannels {
				channels[ch][i] = int32(int8(pcm[pos]))
				pos++
			}
		}
	case Depth12, Depth16:
		for i := range blockSize {
			for ch := range nChannels {
				//nolint:gosec // uint16-to-int16 reinterpretation.
				channels[ch][i] = int32(int16(binary.LittleEndian.Uint16(pcm[pos:])))
				pos += 2
			}
		}
	case Depth20, Depth24:
		for i := range blockSize {
			for ch := range nChannels {
				s := int32(pcm[pos]) | int32(pcm[pos+1])<<8 | int32(pcm[pos+2])<<16
				// Sign-extend from 24-bit.
				if s&sign24Bit != 0 {
					s |= ^mask24Bit
				}

				channels[ch][i] = s
				pos += 3
			}
		}
	case Depth32:
		for i := range blockSize {
			for ch := range nChannels {
				channels[ch][i] = int32( //nolint:gosec // uint32-to-int32 reinterpretation.
					binary.LittleEndian.Uint32(pcm[pos:]),
				)
				pos += 4
			}
		}
	default:
		panic(fmt.Sprintf("flac: deinterleave called with unsupported bit depth %d", depth))
	}
}

// buildFrame constructs a FLAC frame from per-channel int32 samples.
func buildFrame(channels [][]int32, blockSize int, format PCMFormat) *frame.Frame {
	nChannels := len(channels)
	chanAssignment := frame.Channels(nChannels - 1) //nolint:gosec // nChannels is 1-8, always >= 1.

	subframes := make([]*frame.Subframe, nChannels)
	for ch := range nChannels {
		subframes[ch] = &frame.Subframe{
			SubHeader: frame.SubHeader{
				Pred: frame.PredVerbatim,
			},
			Samples:  channels[ch],
			NSamples: blockSize,
		}
	}

	return &frame.Frame{
		Header: frame.Header{
			HasFixedBlockSize: true,
			BlockSize:         uint16(blockSize),         //nolint:gosec // blockSize <= 4096, fits uint16.
			SampleRate:        uint32(format.SampleRate), //nolint:gosec // SampleRate is always positive.
			Channels:          chanAssignment,
			BitsPerSample:     uint8(format.BitDepth), //nolint:gosec // BitDepth is 4-32, fits uint8.
		},
		Subframes: subframes,
	}
}
