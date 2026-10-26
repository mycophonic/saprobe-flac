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

package tests_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"runtime"
	"testing"

	flac "github.com/mycophonic/saprobe-flac"
)

// FuzzDecoder feeds arbitrary bytes to the stream decoder. A stream may be
// refused at NewDecoder (ErrReadFailure, or ErrBitDepth) or mid-stream
// (ErrReadFailure); it may never panic, hang, or yield PCM that is not
// whole samples of the format it declared, nor more samples than its
// StreamInfo declares. The seeds are streams the package's own encoder
// writes at several depths and channel counts, each also cut short and with
// a byte flipped, and the two signatures alone.
func FuzzDecoder(f *testing.F) {
	for _, c := range []struct{ depth, channels int }{{8, 1}, {16, 2}, {24, 2}, {32, 1}, {16, 8}} {
		pcm := make([]byte, 40*c.channels*flac.BitDepth(c.depth).BytesPerSample())
		for i := range pcm {
			pcm[i] = byte(i*37 + i/7)
		}

		var buf bytes.Buffer

		format := flac.PCMFormat{SampleRate: 44100, BitDepth: flac.BitDepth(c.depth), Channels: uint(c.channels)}
		if err := flac.Encode(&buf, pcm, format); err != nil {
			f.Fatal(err)
		}

		data := buf.Bytes()
		f.Add(data)
		f.Add(data[:len(data)*2/3])

		flipped := bytes.Clone(data)
		flipped[len(flipped)/2] ^= 0x55
		f.Add(flipped)
	}

	f.Add([]byte("fLaC"))
	f.Add([]byte("ID3"))

	// Silence: 16 MiB of 8-channel 32-bit zeros, a few kilobytes of stream,
	// the ratio the allocation bound must have room for.
	var silence bytes.Buffer
	if err := flac.Encode(
		&silence,
		make([]byte, 16<<20),
		flac.PCMFormat{SampleRate: 48000, BitDepth: flac.Depth32, Channels: 8},
	); err != nil {
		f.Fatal(err)
	}

	f.Add(silence.Bytes())

	f.Fuzz(func(t *testing.T, data []byte) {
		// What a decode allocates is bounded by the input and the output,
		// not by the lengths the stream declares: the frame buffers of the
		// widest block, a modest multiple of the bytes read, and the PCM
		// collected (FLAC turns a few kilobytes of silence into megabytes,
		// and io.ReadAll grows by doubling).
		var decoded int

		var before runtime.MemStats

		runtime.ReadMemStats(&before)

		defer func() {
			if raceEnabled {
				return
			}

			var after runtime.MemStats

			runtime.ReadMemStats(&after)

			bound := uint64(16<<20) + 64*uint64(len(data)) + 4*uint64(decoded)
			if got := after.TotalAlloc - before.TotalAlloc; got > bound {
				t.Fatalf("decoding %d bytes into %d allocated %d bytes (bound %d)", len(data), decoded, got, bound)
			}
		}()

		dec, err := flac.NewDecoder(bytes.NewReader(data))
		if err != nil {
			if !errors.Is(err, flac.ErrReadFailure) && !errors.Is(err, flac.ErrBitDepth) {
				t.Fatalf("refused with an error outside the contract: %v", err)
			}

			return
		}

		defer func() { _ = dec.Close() }()

		format := dec.Format()
		frameBytes := int(format.Channels) * format.BitDepth.BytesPerSample()

		if format.Channels < 1 || format.Channels > 8 || frameBytes == 0 {
			t.Fatalf("format %+v", format)
		}

		// A hostile header can declare an enormous stream: one case reads
		// at most this much.
		const limit = 32 << 20

		pcm, err := io.ReadAll(io.LimitReader(dec, limit))
		decoded = len(pcm)

		if err != nil && !errors.Is(err, flac.ErrReadFailure) {
			t.Fatalf("Read failed outside the contract: %v", err)
		}

		if len(pcm)%frameBytes != 0 {
			t.Fatalf("%d bytes of PCM are not whole %d-byte samples", len(pcm), frameBytes)
		}

		if total := declaredSamples(data); total > 0 && uint64(len(pcm)/frameBytes) > total {
			t.Fatalf("%d samples decoded, StreamInfo declares %d", len(pcm)/frameBytes, total)
		}

		// A clean end stays an end.
		if err == nil && len(pcm) < limit {
			if n, again := dec.Read(make([]byte, 16)); n != 0 || !errors.Is(again, io.EOF) {
				t.Fatalf("after the end, Read gave %d bytes and %v", n, again)
			}
		}
	})
}

// declaredSamples is StreamInfo's total sample count when data starts with
// the FLAC signature and a StreamInfo block, else 0.
func declaredSamples(data []byte) uint64 {
	if len(data) < 42 || string(data[:4]) != "fLaC" || data[4]&0x7F != 0 {
		return 0
	}

	return uint64(data[21]&0x0F)<<32 | uint64(binary.BigEndian.Uint32(data[22:26]))
}
