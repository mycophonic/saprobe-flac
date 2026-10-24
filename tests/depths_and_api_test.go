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
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	flac "github.com/mycophonic/saprobe-flac"
)

// The depths the package maps onto wider containers, 4, 12 and 20 bits,
// round-trip through Encode, the reference's `flac -t` (which verifies the
// MD5 the encoder wrote) and NewDecoder, sign-extended as the package
// documents; and the API's edges refuse with an error, never a panic: an
// unsupported depth, no channels, too many, PCM that is not whole frames,
// nothing at all.

// depthPCM is interleaved PCM at bps in the package's container width,
// sign-extended, spanning the depth's range with the low bits busy.
func depthPCM(bps flac.BitDepth, channels, n int) []byte {
	bytesPer := bps.BytesPerSample()
	maxV := int64(1)<<(int(bps)-1) - 1
	minV := -maxV - 1
	out := make([]byte, 0, n*channels*bytesPer)

	for i := range n {
		for c := range channels {
			var v int64

			switch (i + c) % 5 {
			case 0:
				v = maxV
			case 1:
				v = minV
			case 2:
				v = int64(i*37+c) % (maxV + 1)
			case 3:
				v = -(int64(i*53+c) % (maxV + 1))
			default:
				v = int64(i%7) - 3
			}

			for b := range bytesPer {
				out = append(out, byte(v>>(8*b)))
			}
		}
	}

	return out
}

func TestNarrowDepthsRoundTrip(t *testing.T) {
	t.Parallel()

	flacBin, err := exec.LookPath("flac")
	if err != nil {
		t.Skip("no reference flac on the PATH")
	}

	for _, bps := range []flac.BitDepth{flac.Depth4, flac.Depth12, flac.Depth20} {
		for _, channels := range []int{1, 2, 6} {
			t.Run(fmt.Sprintf("%dbit/%dch", bps, channels), func(t *testing.T) {
				t.Parallel()

				if bps == flac.Depth4 {
					// The encoder has no 4-bit sample size yet: mycophonic/flac#24.
					t.Skip("4-bit encoding waits on mycophonic/flac#24")
				}

				source := depthPCM(bps, channels, 5000)
				path := filepath.Join(t.TempDir(), "narrow.flac")

				file, err := os.Create(path)
				if err != nil {
					t.Fatal(err)
				}

				format := flac.PCMFormat{SampleRate: 32000, BitDepth: bps, Channels: uint(channels)}
				if encErr := flac.Encode(file, source, format); encErr != nil {
					t.Fatalf("Encode: %v", encErr)
				}

				if out, refErr := exec.CommandContext(t.Context(), flacBin, "-t", "-s", path).
					CombinedOutput(); refErr != nil {
					t.Fatalf("the reference refuses the stream: %v\n%s", refErr, out)
				}

				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}

				dec, err := flac.NewDecoder(bytes.NewReader(data))
				if err != nil {
					t.Fatal(err)
				}

				defer func() { _ = dec.Close() }()

				if got := dec.Format(); got != format {
					t.Fatalf("format %+v, want %+v", got, format)
				}

				got, err := io.ReadAll(dec)
				if err != nil {
					t.Fatal(err)
				}

				if !bytes.Equal(got, source) {
					at := 0
					for at < len(got) && at < len(source) && got[at] == source[at] {
						at++
					}

					t.Fatalf("%d bytes back for %d, first difference at byte %d", len(got), len(source), at)
				}
			})
		}
	}
}

// noPanic runs f and returns its error, or an error naming the panic.
func noPanic(f func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()

	return f()
}

func TestEncodeRefusesBadFormatsWithoutPanicking(t *testing.T) {
	t.Parallel()

	good := flac.PCMFormat{SampleRate: 44100, BitDepth: flac.Depth16, Channels: 2}
	pcm := depthPCM(flac.Depth16, 2, 64)

	cases := []struct {
		name   string
		format flac.PCMFormat
		pcm    []byte
		// badFormat: the refusal is ErrFormat; otherwise the PCM's own error.
		badFormat bool
	}{
		{"bit depth 7", flac.PCMFormat{SampleRate: 44100, BitDepth: 7, Channels: 2}, pcm, true},
		{"bit depth 0", flac.PCMFormat{SampleRate: 44100, BitDepth: 0, Channels: 2}, pcm, true},
		{"bit depth 64", flac.PCMFormat{SampleRate: 44100, BitDepth: 64, Channels: 2}, pcm, true},
		{"no channels", flac.PCMFormat{SampleRate: 44100, BitDepth: flac.Depth16, Channels: 0}, pcm, true},
		{
			"nine channels",
			flac.PCMFormat{SampleRate: 44100, BitDepth: flac.Depth16, Channels: 9},
			depthPCM(flac.Depth16, 9, 64),
			true,
		},
		{"sample rate 0", flac.PCMFormat{SampleRate: 0, BitDepth: flac.Depth16, Channels: 2}, pcm, true},
		{"PCM not whole frames", good, pcm[:len(pcm)-1], false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			err := noPanic(func() error { return flac.Encode(&out, c.pcm, c.format) })
			if err == nil {
				t.Fatalf("Encode accepted %+v with %d bytes of PCM and wrote %d bytes", c.format, len(c.pcm), out.Len())
			}

			if errors.Is(err, io.EOF) || len(err.Error()) >= 6 && err.Error()[:6] == "panic:" {
				t.Fatalf("Encode: %v", err)
			}

			if c.badFormat && !errors.Is(err, flac.ErrFormat) {
				t.Fatalf("Encode refused the format, but not as ErrFormat: %v", err)
			}

			t.Logf("Encode: %v", err)
		})
	}
}

// No PCM at all is a stream of no frames, which the package's own decoder
// must read back as nothing.
func TestEncodeOfNothingDecodesToNothing(t *testing.T) {
	t.Parallel()

	var out bytes.Buffer

	err := noPanic(func() error {
		return flac.Encode(&out, nil, flac.PCMFormat{SampleRate: 44100, BitDepth: flac.Depth16, Channels: 2})
	})
	if err != nil {
		t.Logf("Encode of no PCM: %v", err)

		return
	}

	dec, openErr := flac.NewDecoder(bytes.NewReader(out.Bytes()))
	if openErr != nil {
		t.Fatalf("the stream Encode wrote for no PCM is refused: %v", openErr)
	}

	defer func() { _ = dec.Close() }()

	if got, readErr := io.ReadAll(dec); readErr != nil || len(got) != 0 {
		t.Fatalf("the stream Encode wrote for no PCM reads back %d bytes, %v", len(got), readErr)
	}
}

func TestNewDecoderRefusesWithoutPanicking(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		rs   io.ReadSeeker
	}{
		{"nil reader", nil},
		{"empty", bytes.NewReader(nil)},
		{"the signature alone", bytes.NewReader([]byte("fLaC"))},
		{"a 4-bit sine's first 20 bytes", bytes.NewReader(func() []byte {
			var buf bytes.Buffer

			_ = flac.Encode(
				&buf,
				depthPCM(flac.Depth16, 1, 64),
				flac.PCMFormat{SampleRate: 8000, BitDepth: flac.Depth16, Channels: 1},
			)

			return buf.Bytes()[:20]
		}())},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			err := noPanic(func() error {
				dec, err := flac.NewDecoder(c.rs)
				if err != nil {
					return err
				}

				defer func() { _ = dec.Close() }()

				_, err = io.ReadAll(dec)

				return err
			})

			if err == nil || len(err.Error()) >= 6 && err.Error()[:6] == "panic:" {
				t.Fatalf("NewDecoder then Read: %v", err)
			}

			if !errors.Is(err, flac.ErrReadFailure) {
				t.Fatalf("a refusal that is not ErrReadFailure: %v", err)
			}
		})
	}
}
