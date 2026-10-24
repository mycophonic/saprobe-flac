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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	goflac "github.com/mycophonic/flac"
	"github.com/mycophonic/flac/frame"
	"github.com/mycophonic/flac/meta"

	flac "github.com/mycophonic/saprobe-flac"
)

// Every stereo channel assignment at every stereo depth, at the sample
// extremes where the side channel needs one bit more than the samples (RFC
// 9639, section 4.2): the reference encoder's mid/side and independent
// streams, and left/side and side/right streams the fork's encoder writes
// on request. Each decodes through saprobe-flac to the source PCM, and to
// what the reference `flac -d` decodes.

// stereoPatterns are two-channel sample shapes at the depth's extremes.
func stereoPatterns(bps int) map[string][][2]int32 {
	maxV := int32(1)<<(bps-1) - 1
	minV := -maxV - 1

	const n = 64

	constant := func(l, r int32) [][2]int32 {
		out := make([][2]int32, n)
		for i := range out {
			out[i] = [2]int32{l, r}
		}

		return out
	}

	alternating := make([][2]int32, n)
	ramp := make([][2]int32, n)

	for i := range n {
		if i%2 == 0 {
			alternating[i] = [2]int32{maxV, minV}
		} else {
			alternating[i] = [2]int32{minV, maxV}
		}

		step := int64(maxV-minV) / (n - 1)
		ramp[i] = [2]int32{int32(int64(minV) + step*int64(i)), int32(int64(maxV) - step*int64(i))}
	}

	return map[string][][2]int32{
		"max,min":     constant(maxV, minV),
		"min,max":     constant(minV, maxV),
		"max,max":     constant(maxV, maxV),
		"min,min":     constant(minV, minV),
		"max,zero":    constant(maxV, 0),
		"zero,min":    constant(0, minV),
		"alternating": alternating,
		"ramp":        ramp,
	}
}

// rawPCM is the pattern as interleaved little-endian signed PCM at bps.
func rawPCM(samples [][2]int32, bps int) []byte {
	bytesPer := bps / 8
	out := make([]byte, 0, len(samples)*2*bytesPer)

	for _, pair := range samples {
		for _, v := range pair {
			var b [4]byte

			binary.LittleEndian.PutUint32(b[:], uint32(v))
			out = append(out, b[:bytesPer]...)
		}
	}

	return out
}

// referenceEncode runs the reference encoder on raw PCM with the options
// given, into dst.
func referenceEncode(t *testing.T, flacBin, src, dst string, bps int, options ...string) {
	t.Helper()

	args := append([]string{
		"-f", "-s", "--force-raw-format", "--sign=signed", "--endian=little", "--channels=2",
		fmt.Sprintf("--bps=%d", bps), "--sample-rate=44100", "-0",
	}, options...)
	args = append(args, "-o", dst, src)

	if out, err := exec.CommandContext(t.Context(), flacBin, args...).CombinedOutput(); err != nil {
		t.Fatalf("flac %v: %v\n%s", options, err, out)
	}
}

// forkEncode writes the samples as verbatim subframes under the channel
// assignment asked, through the fork's encoder.
func forkEncode(t *testing.T, dst string, samples [][2]int32, bps int, channels frame.Channels) {
	t.Helper()

	file, err := os.Create(dst)
	if err != nil {
		t.Fatal(err)
	}

	info := &meta.StreamInfo{
		BlockSizeMin: uint16(len(samples)), BlockSizeMax: uint16(len(samples)),
		SampleRate: 44100, NChannels: 2, BitsPerSample: uint8(bps), NSamples: uint64(len(samples)),
	}

	enc, err := goflac.NewEncoder(file, info)
	if err != nil {
		t.Fatal(err)
	}

	left, right := make([]int32, len(samples)), make([]int32, len(samples))
	for i, pair := range samples {
		left[i], right[i] = pair[0], pair[1]
	}

	f := &frame.Frame{
		Header: frame.Header{
			HasFixedBlockSize: true, BlockSize: uint16(len(samples)), SampleRate: 44100,
			Channels: channels, BitsPerSample: uint8(bps),
		},
		Subframes: []*frame.Subframe{
			{SubHeader: frame.SubHeader{Pred: frame.PredVerbatim}, Samples: left, NSamples: len(samples)},
			{SubHeader: frame.SubHeader{Pred: frame.PredVerbatim}, Samples: right, NSamples: len(samples)},
		},
	}

	if err := enc.WriteFrame(f); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}

	if err := enc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// firstAssignment is the channel assignment of the file's first frame.
func firstAssignment(t *testing.T, path string) frame.Channels {
	t.Helper()

	stream, err := goflac.ParseFile(path)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = stream.Close() }()

	first, err := stream.ParseNext()
	if err != nil {
		t.Fatal(err)
	}

	return first.Channels
}

func TestStereoAssignmentsAtTheExtremes(t *testing.T) {
	t.Parallel()

	flacBin, err := exec.LookPath("flac")
	if err != nil {
		t.Skip("no reference flac on the PATH")
	}

	// want is the assignment a case forces; the reference encoder picks
	// its own by estimate, so its cases take whatever it chose (anyOf).
	const anyOf = frame.Channels(255)

	type encoding struct {
		name   string
		encode func(t *testing.T, src, dst string, samples [][2]int32, bps int)
		want   frame.Channels
	}

	encodings := []encoding{
		{"reference mid-side", func(t *testing.T, src, dst string, _ [][2]int32, bps int) {
			t.Helper()
			referenceEncode(t, flacBin, src, dst, bps, "-m")
		}, anyOf},
		{"reference independent", func(t *testing.T, src, dst string, _ [][2]int32, bps int) {
			t.Helper()
			referenceEncode(t, flacBin, src, dst, bps, "--no-mid-side")
		}, anyOf},
		{"left-side", func(t *testing.T, _, dst string, samples [][2]int32, bps int) {
			t.Helper()
			forkEncode(t, dst, samples, bps, frame.ChannelsLeftSide)
		}, frame.ChannelsLeftSide},
		{"side-right", func(t *testing.T, _, dst string, samples [][2]int32, bps int) {
			t.Helper()
			forkEncode(t, dst, samples, bps, frame.ChannelsSideRight)
		}, frame.ChannelsSideRight},
		{"mid-side", func(t *testing.T, _, dst string, samples [][2]int32, bps int) {
			t.Helper()
			forkEncode(t, dst, samples, bps, frame.ChannelsMidSide)
		}, frame.ChannelsMidSide},
	}

	for _, bps := range []int{16, 24, 32} {
		for name, samples := range stereoPatterns(bps) {
			for _, enc := range encodings {
				t.Run(fmt.Sprintf("%dbit/%s/%s", bps, enc.name, name), func(t *testing.T) {
					t.Parallel()

					dir := t.TempDir()
					src := filepath.Join(dir, "source.raw")
					dst := filepath.Join(dir, "encoded.flac")
					source := rawPCM(samples, bps)

					if err := os.WriteFile(src, source, 0o600); err != nil {
						t.Fatal(err)
					}

					enc.encode(t, src, dst, samples, bps)

					// At 32 bits a decorrelated side channel would need 33, so the
					// encoder writes the pair as independent channels, the header
					// saying so, whatever assignment it was asked for.
					want := enc.want
					if bps == 32 && want != anyOf {
						want = frame.ChannelsLR
					}

					assignment := firstAssignment(t, dst)
					if want != anyOf && assignment != want {
						t.Fatalf("the stream's first frame uses assignment %d, the case wants %d", assignment, want)
					}

					t.Logf("channel assignment %d", assignment)

					// The reference decoder is the oracle; the source is the
					// truth it must meet, which also holds the encoders.
					reference, err := flacBinaryDecodeRaw(flacBin, dst)
					if err != nil {
						t.Fatalf("flac -d: %v", err)
					}

					if !bytes.Equal(reference, source) {
						t.Fatalf(
							"the reference decodes the stream to other samples than the source: the encoder lost them\n%s",
							diffSamples(reference, source, bps),
						)
					}

					got, format, err := decodeSaprobe(dst)
					if err != nil {
						t.Fatalf("saprobe: %v", err)
					}

					if format.BitDepth != flac.BitDepth(bps) || format.Channels != 2 {
						t.Fatalf("format %+v", format)
					}

					if !bytes.Equal(got, reference) {
						t.Fatalf(
							"saprobe decodes other samples than the reference\n%s",
							diffSamples(got, reference, bps),
						)
					}
				})
			}
		}
	}
}

// diffSamples names the first differing sample pair.
func diffSamples(got, want []byte, bps int) string {
	bytesPer := bps / 8
	frameBytes := 2 * bytesPer

	if len(got) != len(want) {
		return fmt.Sprintf("%d bytes against %d", len(got), len(want))
	}

	for i := 0; i+frameBytes <= len(got); i += frameBytes {
		if !bytes.Equal(got[i:i+frameBytes], want[i:i+frameBytes]) {
			return fmt.Sprintf("sample %d: got % x, want % x", i/frameBytes, got[i:i+frameBytes], want[i:i+frameBytes])
		}
	}

	return "no difference"
}
