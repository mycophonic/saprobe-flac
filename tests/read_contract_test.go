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
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	flac "github.com/mycophonic/saprobe-flac"
)

// The decoder as an io.Reader, and the stream's edges the reference decoder
// decides: Read yields the same PCM whatever the buffer and ends with a
// sticky EOF; an ID3v2 tag before the stream is skipped as libFLAC skips it;
// trailing bytes after the last frame are tolerated when StreamInfo says how
// many samples there are (the ID3v1 case) and refused when it does not.

// sineFile encodes a short stereo sine through the reference encoder, with
// the total sample count in StreamInfo (a file) or without (a pipe).
func sineFile(t *testing.T, flacBin string, knownLength bool) (path string, source []byte) {
	t.Helper()

	dir := t.TempDir()
	source = make([]byte, 0, 9000*4)

	for i := range 9000 {
		v := int16(20000 * math.Sin(float64(i)/7))
		source = append(source, byte(v), byte(v>>8), byte(-v), byte(-v>>8))
	}

	src := filepath.Join(dir, "source.raw")
	if err := os.WriteFile(src, source, 0o600); err != nil {
		t.Fatal(err)
	}

	path = filepath.Join(dir, "sine.flac")
	args := []string{
		"-f", "-s", "--force-raw-format", "--sign=signed", "--endian=little", "--channels=2", "--bps=16",
		"--sample-rate=44100",
	}

	if knownLength {
		args = append(args, "-o", path, src)

		if out, err := exec.CommandContext(t.Context(), flacBin, args...).CombinedOutput(); err != nil {
			t.Fatalf("flac: %v\n%s", err, out)
		}

		return path, source
	}

	// Through pipes on both sides the encoder cannot seek back to write
	// the count; a file on either side, it does.
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}

	cmd := exec.CommandContext(t.Context(), flacBin, append(args, "-o", "-", "-")...)
	cmd.Stdin = bytes.NewReader(source)
	cmd.Stdout = out

	var stderr bytes.Buffer

	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("flac from a pipe: %v\n%s", err, stderr.String())
	}

	if err := out.Close(); err != nil {
		t.Fatal(err)
	}

	return path, source
}

func TestReadIsTheSameWhateverTheBuffer(t *testing.T) {
	t.Parallel()

	flacBin, err := exec.LookPath("flac")
	if err != nil {
		t.Skip("no reference flac on the PATH")
	}

	path, source := sineFile(t, flacBin, true)

	// The sizes include the two that make the last Read deliver exactly one
	// byte, and one byte short of everything, where an off-by-one at the end
	// would drop or invent a byte.
	for _, size := range []int{1, 2, 3, 4, 5, 1000, 4095 * 4, 4096 * 4, 4097 * 4, 1 << 20, len(source) - 1, len(source) + 1} {
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}

		dec, err := flac.NewDecoder(file)
		if err != nil {
			t.Fatal(err)
		}

		var got bytes.Buffer

		buf := make([]byte, size)

		for {
			n, err := dec.Read(buf)
			got.Write(buf[:n])

			if errors.Is(err, io.EOF) {
				break
			}

			if err != nil {
				t.Fatalf("buffer %d: %v", size, err)
			}
		}

		if !bytes.Equal(got.Bytes(), source) {
			t.Fatalf("buffer %d: %d bytes, want %d, or the bytes differ", size, got.Len(), len(source))
		}

		if n, err := dec.Read(buf); n != 0 || !errors.Is(err, io.EOF) {
			t.Fatalf("buffer %d: after EOF, %d bytes and %v", size, n, err)
		}

		if n, err := dec.Read(nil); n != 0 || err != nil {
			t.Fatalf("buffer %d: a zero-length read gave %d, %v", size, n, err)
		}

		_ = dec.Close()
		_ = file.Close()
	}
}

// decodeBytesWith decodes a stream held in memory through reads of size
// bytes, the last read's count included in what comes back.
func decodeBytesWith(data []byte, size int) ([]byte, error) {
	dec, err := flac.NewDecoder(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	defer func() { _ = dec.Close() }()

	var out []byte

	buf := make([]byte, size)

	for {
		n, err := dec.Read(buf)
		out = append(out, buf[:n]...)

		// The bare io.EOF, as io.ReadAll takes it: a refusal that wraps
		// io.EOF is not a clean end.
		if err == io.EOF { //nolint:errorlint // the end is the bare sentinel, by io.Reader's contract
			return out, nil
		}

		if err != nil {
			return out, err
		}
	}
}

// decodeBytes decodes a stream held in memory.
func decodeBytes(data []byte) ([]byte, error) {
	dec, err := flac.NewDecoder(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	defer func() { _ = dec.Close() }()

	return io.ReadAll(dec)
}

// frameOffsets are the byte offsets of the stream's frames, found by the
// sync word 0xFFF8 at a byte boundary after the metadata, in a stream the
// reference encoder wrote with fixed block sizes.
func frameOffsets(data []byte) []int {
	var offsets []int

	// Past "fLaC" and the metadata blocks.
	pos := 4
	for {
		if pos+4 > len(data) {
			return nil
		}

		last := data[pos]&0x80 != 0
		length := int(data[pos+1])<<16 | int(data[pos+2])<<8 | int(data[pos+3])
		pos += 4 + length

		if last {
			break
		}
	}

	for i := pos; i+1 < len(data); i++ {
		if data[i] == 0xFF && data[i+1]&0xFE == 0xF8 {
			offsets = append(offsets, i)
		}
	}

	return offsets
}

// overwriteSync zeroes the sync word of frame n.
func overwriteSync(data []byte, n int) []byte {
	out := bytes.Clone(data)
	if offsets := frameOffsets(data); n < len(offsets) {
		out[offsets[n]], out[offsets[n]+1] = 0, 0
	}

	return out
}

// insertBefore inserts junk before frame n.
func insertBefore(data []byte, n int, junk []byte) []byte {
	offsets := frameOffsets(data)
	if n >= len(offsets) {
		return data
	}

	out := bytes.Clone(data[:offsets[n]])
	out = append(out, junk...)

	return append(out, data[offsets[n]:]...)
}

func TestStreamEdgesAgainstTheReference(t *testing.T) {
	t.Parallel()

	flacBin, err := exec.LookPath("flac")
	if err != nil {
		t.Skip("no reference flac on the PATH")
	}

	known, source := sineFile(t, flacBin, true)
	unknown, _ := sineFile(t, flacBin, false)

	knownData, err := os.ReadFile(known)
	if err != nil {
		t.Fatal(err)
	}

	unknownData, err := os.ReadFile(unknown)
	if err != nil {
		t.Fatal(err)
	}

	// The sync scan must see the three frames the encoder wrote and nothing
	// else, or the damage cases below would test something other than they
	// say.
	if n := len(frameOffsets(knownData)); n != 3 {
		t.Fatalf("the sync scan finds %d frames in the sine stream, want 3", n)
	}

	id3v1 := append([]byte("TAG"), bytes.Repeat([]byte{' '}, 125)...)
	junk := bytes.Repeat([]byte{0xA5, 0x5A, 0xFF, 0x00}, 75)
	// A minimal ID3v2.3 tag: header, then ten bytes of padding.
	id3v2 := append([]byte{'I', 'D', '3', 3, 0, 0, 0, 0, 0, 10}, make([]byte, 10)...)

	cases := []struct {
		name string
		data []byte
		// want is the PCM, or nil for a refusal.
		want []byte
	}{
		{"the stream as encoded", knownData, source},
		{"an unknown length as encoded", unknownData, source},
		{"an ID3v2 tag before the stream", slices.Concat(id3v2, knownData), source},
		// The reference refuses trailing bytes (FLAC__STREAM_DECODER_ERROR_STATUS_LOST_SYNC);
		// the decoder tolerates them once every declared sample is out,
		// which is the ID3v1 case, and refuses them when the count is
		// unknown, as the reference does.
		{"an ID3v1 tag after a known length", slices.Concat(knownData, id3v1), source},
		{"junk after a known length", slices.Concat(knownData, junk), source},
		{"an ID3v1 tag after an unknown length", slices.Concat(unknownData, id3v1), nil},
		{"junk after an unknown length", slices.Concat(unknownData, junk), nil},
		{"a stream cut inside a frame", knownData[:len(knownData)-7], nil},
		{"a stream cut inside the metadata", knownData[:30], nil},
		// Damage in the middle of the stream is never silence: a frame's
		// sync word overwritten, and junk inserted between two frames.
		{"a middle frame's sync overwritten", overwriteSync(knownData, 1), nil},
		{"junk inserted between two frames", insertBefore(knownData, 1, junk), nil},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			got, err := decodeBytes(c.data)

			switch {
			case c.want == nil && err == nil:
				t.Fatalf("decoded %d bytes, want a refusal", len(got))
			case c.want == nil:
				if !errors.Is(err, flac.ErrReadFailure) {
					t.Fatalf("a refusal that is not ErrReadFailure: %v", err)
				}
			case err != nil:
				t.Fatalf("refused: %v", err)
			case !bytes.Equal(got, c.want):
				t.Fatalf("%d bytes, want %d, or the bytes differ", len(got), len(c.want))
			}

			if c.want != nil && len(got) != len(c.want) {
				t.Fatalf("%d bytes, want %d", len(got), len(c.want))
			}

			// The same through reads that leave one byte for the last one, and
			// through one read that takes everything so the next starts on the
			// trailing bytes with nothing in hand: where an end after trailing
			// bytes is most easily off by one.
			if c.want != nil {
				for _, size := range []int{len(c.want) - 1, len(c.want)} {
					sized, err := decodeBytesWith(c.data, size)
					if err != nil || !bytes.Equal(sized, c.want) {
						t.Fatalf("through %d-byte reads: %d bytes, %v", size, len(sized), err)
					}
				}
			}

			// Where the reference decodes, it decodes the same.
			path := filepath.Join(t.TempDir(), "case.flac")
			if err := os.WriteFile(path, c.data, 0o600); err != nil {
				t.Fatal(err)
			}

			reference, refErr := flacBinaryDecodeRaw(flacBin, path)
			if refErr == nil && c.want != nil && !bytes.Equal(reference, c.want) {
				t.Fatalf("the reference decodes %d bytes that differ from the source", len(reference))
			}

			t.Logf("reference: %v", refErr == nil)
		})
	}
}

// closeFailing is a source whose Close fails; the decoder's Close must say
// so.
type closeFailing struct{ *bytes.Reader }

var errSourceClose = errors.New("the source would not close")

func (closeFailing) Close() error { return errSourceClose }

func TestCloseReportsTheSourcesFailure(t *testing.T) {
	t.Parallel()

	flacBin, err := exec.LookPath("flac")
	if err != nil {
		t.Skip("no reference flac on the PATH")
	}

	path, _ := sineFile(t, flacBin, true)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	dec, err := flac.NewDecoder(closeFailing{bytes.NewReader(data)})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := io.ReadAll(dec); err != nil {
		t.Fatal(err)
	}

	if err := dec.Close(); !errors.Is(err, errSourceClose) {
		t.Fatalf("Close: %v, want the source's failure", err)
	}
}
