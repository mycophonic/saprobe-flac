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
	"slices"

	goflac "github.com/mewkiz/flac"
	"github.com/mewkiz/flac/frame"
)

//nolint:gochecknoglobals
var flacBitDepths = []BitDepth{
	Depth4,
	Depth8,
	Depth12,
	Depth16,
	Depth20,
	Depth24,
	Depth32,
}

var (
	// ErrBitDepth is returned when a FLAC stream has an unsupported bit depth.
	ErrBitDepth = errors.New("unsupported bit depth")

	// ErrReadFailure is returned when reading from the FLAC stream fails.
	ErrReadFailure = errors.New("read failure")
)

// Decoder streams decoded PCM from a FLAC source.
type Decoder struct {
	stream         *goflac.Stream
	format         PCMFormat
	nChannels      int
	bytesPerSample int
	bitDepth       BitDepth

	// Per-frame buffer: filled by ParseNext + interleave, drained by Read.
	buf    []byte
	bufOff int
	eof    bool
}

// NewDecoder opens a FLAC stream and returns a streaming decoder.
// The caller should call Close when done.
func NewDecoder(rs io.ReadSeeker) (*Decoder, error) {
	stream, err := goflac.New(rs)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrReadFailure, err)
	}

	info := stream.Info
	nChannels := int(info.NChannels)

	bitDepth := BitDepth(info.BitsPerSample)
	if !slices.Contains(flacBitDepths, bitDepth) {
		_ = stream.Close()

		return nil, ErrBitDepth
	}

	return &Decoder{
		stream:         stream,
		nChannels:      nChannels,
		bytesPerSample: bitDepth.BytesPerSample(),
		bitDepth:       bitDepth,
		format: PCMFormat{
			SampleRate: int(info.SampleRate),
			BitDepth:   bitDepth,
			Channels:   uint(nChannels), //nolint:gosec // nChannels comes from uint8, always fits in uint.
		},
	}, nil
}

// Format returns the PCM output format.
func (d *Decoder) Format() PCMFormat { return d.format }

// Read reads decoded PCM bytes from the FLAC stream.
func (d *Decoder) Read(p []byte) (int, error) { //nolint:varnamelen // p is idiomatic for io.Reader.Read
	total := 0

	for len(p) > 0 {
		// Drain buffered frame data.
		if d.bufOff < len(d.buf) {
			n := copy(p, d.buf[d.bufOff:])
			d.bufOff += n
			total += n
			p = p[n:]

			continue
		}

		if d.eof {
			if total > 0 {
				return total, nil
			}

			return 0, io.EOF
		}

		// Decode next frame.
		audioFrame, parseErr := d.stream.ParseNext()
		if errors.Is(parseErr, io.EOF) {
			d.eof = true

			if total > 0 {
				return total, nil
			}

			return 0, io.EOF
		}

		if parseErr != nil {
			return total, fmt.Errorf("%w: %w", ErrReadFailure, parseErr)
		}

		blockSize := int(audioFrame.BlockSize)
		frameBytes := blockSize * d.nChannels * d.bytesPerSample

		// Grow frame buffer if needed.
		if cap(d.buf) < frameBytes {
			d.buf = make([]byte, frameBytes)
		} else {
			d.buf = d.buf[:frameBytes]
		}

		interleave(d.buf, audioFrame.Subframes, blockSize, d.nChannels, d.bitDepth)
		d.bufOff = 0
	}

	return total, nil
}

// Close releases resources held by the FLAC stream.
func (d *Decoder) Close() error {
	if err := d.stream.Close(); err != nil {
		return fmt.Errorf("closing flac stream: %w", err)
	}

	return nil
}

// Decode reads a FLAC stream and decodes it to interleaved little-endian signed PCM bytes.
// Native bit depth is preserved (16-bit FLAC produces s16le, 24-bit produces s24le, etc.).
func Decode(rs io.ReadSeeker) ([]byte, PCMFormat, error) {
	dec, err := NewDecoder(rs)
	if err != nil {
		return nil, PCMFormat{}, err
	}
	defer dec.Close()

	pcm, err := io.ReadAll(dec)
	if err != nil {
		return nil, PCMFormat{}, fmt.Errorf("decoding flac: %w", err)
	}

	return pcm, dec.Format(), nil
}

// interleave writes decoded subframe samples into dst as interleaved little-endian signed PCM.
//
// Stereo paths use packed writes (PutUint32/PutUint64) to emit both channels per store
// instruction, with bounds-check elimination (BCE) hints so the compiler removes all
// per-iteration bounds checks from the inner loop.
//
//revive:disable-next-line:cognitive-complexity // single switch over 5 bit-depth groups × stereo/multi paths; splitting hurts readability.
func interleave(dst []byte, subframes []*frame.Subframe, blockSize, nChannels int, depth BitDepth) {
	switch depth {
	case Depth4, Depth8:
		// 4-bit sign-extended to 8-bit, 8-bit native. Both stored as 1 byte.
		if nChannels == 2 {
			left := subframes[0].Samples[:blockSize:blockSize]
			right := subframes[1].Samples[:blockSize:blockSize]
			_ = dst[blockSize*2-1] // BCE

			for i, l := range left {
				//nolint:gosec // Intentional int32-to-uint16 packing.
				binary.LittleEndian.PutUint16(dst[i*2:], uint16(uint8(l))|uint16(uint8(right[i]))<<8)
			}
		} else {
			pos := 0

			for i := range blockSize {
				for ch := range nChannels {
					//nolint:gosec // G115: intentional int32→int8 truncation for 4/8-bit PCM sign extension.
					dst[pos] = byte(
						int8(subframes[ch].Samples[i]),
					)
					pos++
				}
			}
		}
	case Depth12, Depth16:
		// 12-bit sign-extended to 16-bit, 16-bit native. Both stored as 2 bytes LE.
		if nChannels == 2 {
			left := subframes[0].Samples[:blockSize:blockSize]
			right := subframes[1].Samples[:blockSize:blockSize]
			_ = dst[blockSize*4-1] // BCE

			for i, l := range left {
				r := right[i]
				//nolint:gosec // G115: intentional int32→uint16 truncation for 12/16-bit stereo PCM packing.
				binary.LittleEndian.PutUint32(dst[i*4:], uint32(uint16(l))|uint32(uint16(r))<<16)
			}
		} else {
			pos := 0

			for i := range blockSize {
				for ch := range nChannels {
					s := subframes[ch].Samples[i]
					dst[pos] = byte(s)
					dst[pos+1] = byte(s >> 8)
					pos += 2
				}
			}
		}
	case Depth20, Depth24:
		// 20-bit sign-extended to 24-bit, 24-bit native. Both stored as 3 bytes LE.
		if nChannels == 2 {
			left := subframes[0].Samples[:blockSize:blockSize]
			right := subframes[1].Samples[:blockSize:blockSize]
			_ = dst[blockSize*6-1] // BCE

			for i, lSample := range left {
				rSample := right[i]
				off := i * 6
				// Pack first sample's 24 bits + second sample's low byte into one uint32.
				//nolint:gosec // G115: intentional int32→uint8 truncation for 20/24-bit stereo PCM packing.
				binary.LittleEndian.PutUint32(
					dst[off:],
					uint32(uint8(lSample))|
						uint32(uint8(lSample>>8))<<8|
						uint32(uint8(lSample>>16))<<16|
						uint32(uint8(rSample))<<24,
				)
				dst[off+4] = byte(rSample >> 8)
				dst[off+5] = byte(rSample >> 16)
			}
		} else {
			pos := 0

			for i := range blockSize {
				for ch := range nChannels {
					s := subframes[ch].Samples[i]
					dst[pos] = byte(s)
					dst[pos+1] = byte(s >> 8)
					dst[pos+2] = byte(s >> 16)
					pos += 3
				}
			}
		}
	case Depth32:
		if nChannels == 2 {
			left := subframes[0].Samples[:blockSize:blockSize]
			right := subframes[1].Samples[:blockSize:blockSize]
			_ = dst[blockSize*8-1] // BCE

			for i, l := range left {
				r := right[i]
				//nolint:gosec // G115: intentional int32→uint32 reinterpretation for 32-bit stereo PCM packing.
				binary.LittleEndian.PutUint64(dst[i*8:], uint64(uint32(l))|uint64(uint32(r))<<32)
			}
		} else {
			pos := 0

			for i := range blockSize {
				for ch := range nChannels {
					s := subframes[ch].Samples[i]
					dst[pos] = byte(s)
					dst[pos+1] = byte(s >> 8)
					dst[pos+2] = byte(s >> 16)
					dst[pos+3] = byte(s >> 24)
					pos += 4
				}
			}
		}
	default:
		panic(fmt.Sprintf("flac: interleave called with unsupported bit depth %d", depth))
	}
}
