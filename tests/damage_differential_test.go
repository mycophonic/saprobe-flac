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
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	flac "github.com/mycophonic/saprobe-flac"
)

// Random damage against the reference decoder. Streams from a corpus are
// damaged in the ways files get damaged (bytes flipped, spans truncated,
// dropped, doubled, overwritten, zeroed, the metadata and the frame
// headers hit on purpose) and decoded by this decoder and by `flac -d`.
// Whatever happens, this decoder must neither panic nor hang; where both
// decode, at the depths the reference writes raw, the PCM must be the
// same; where the reference decodes and this decoder refuses, or the other
// way round, the case is logged for the record. DAMAGE_CASES sets the
// number of cases (default 200); DAMAGE_CORPUS directories (a path list)
// of .flac files to damage on top of the encoder's own sine.

type damageOutcome struct {
	pcm    []byte
	format flac.PCMFormat
	err    error
}

func rawDepth(format flac.PCMFormat) bool {
	raw := []flac.BitDepth{flac.Depth8, flac.Depth16, flac.Depth24, flac.Depth32}

	return slices.Contains(raw, format.BitDepth)
}

// decodeWithDeadline decodes in a goroutine so a hang is a failure, not a
// stuck test.
func decodeWithDeadline(data []byte, deadline time.Duration) (damageOutcome, bool) {
	done := make(chan damageOutcome, 1)

	go func() {
		dec, err := flac.NewDecoder(bytes.NewReader(data))
		if err != nil {
			done <- damageOutcome{err: err}

			return
		}

		defer func() { _ = dec.Close() }()

		pcm, err := io.ReadAll(dec)
		done <- damageOutcome{pcm: pcm, format: dec.Format(), err: err}
	}()

	select {
	case out := <-done:
		return out, true
	case <-time.After(deadline):
		return damageOutcome{}, false
	}
}

// damage returns a damaged copy of data and the name of what was done.
func damage(rng *rand.Rand, data []byte) ([]byte, string) {
	out := bytes.Clone(data)
	if len(out) < 8 {
		return out, "untouched"
	}

	span := func() (int, int) {
		start := rng.IntN(len(out))
		length := 1 + rng.IntN(min(len(out)-start, 1+rng.IntN(4096)))

		return start, length
	}

	switch rng.IntN(8) {
	case 0:
		n := 1 + rng.IntN(8)
		for range n {
			out[rng.IntN(len(out))] ^= byte(1 << rng.IntN(8))
		}

		return out, fmt.Sprintf("%d bits flipped", n)
	case 1:
		at := rng.IntN(len(out))

		return out[:at], fmt.Sprintf("truncated at %d of %d", at, len(out))
	case 2:
		start, length := span()

		return append(out[:start], out[start+length:]...), fmt.Sprintf("%d bytes dropped at %d", length, start)
	case 3:
		start, length := span()
		dup := append([]byte{}, out[start:start+length]...)

		return append(
				out[:start+length],
				append(dup, out[start+length:]...)...,
			), fmt.Sprintf(
				"%d bytes doubled at %d",
				length,
				start,
			)
	case 4:
		start, length := span()
		for i := start; i < start+length; i++ {
			out[i] = byte(rng.IntN(256))
		}

		return out, fmt.Sprintf("%d bytes overwritten at %d", length, start)
	case 5:
		start, length := span()
		for i := start; i < start+length; i++ {
			out[i] = 0
		}

		return out, fmt.Sprintf("%d bytes zeroed at %d", length, start)
	case 6:
		// The metadata: the first kilobyte, or less.
		at := rng.IntN(min(len(out), 1024))
		out[at] ^= byte(1 << rng.IntN(8))

		return out, fmt.Sprintf("a metadata bit flipped at %d", at)
	default:
		// A frame header: the bytes after a sync word.
		offsets := frameOffsets(out)
		if len(offsets) == 0 {
			return out, "untouched"
		}

		at := offsets[rng.IntN(len(offsets))] + rng.IntN(8)
		if at >= len(out) {
			return out, "untouched"
		}

		out[at] ^= byte(1 << rng.IntN(8))

		return out, fmt.Sprintf("a frame header bit flipped at %d", at)
	}
}

func damageCorpus(t *testing.T, flacBin string) map[string][]byte {
	t.Helper()

	corpus := map[string][]byte{}

	for _, known := range []bool{true, false} {
		path, _ := sineFile(t, flacBin, known)

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}

		corpus[fmt.Sprintf("sine known=%v", known)] = data
	}

	for _, dir := range filepath.SplitList(os.Getenv("DAMAGE_CORPUS")) {
		files, _ := filepath.Glob(filepath.Join(dir, "*.flac"))
		for _, path := range files {
			data, err := os.ReadFile(path)
			if err != nil || len(data) > 4<<20 {
				continue
			}

			corpus[filepath.Base(dir)+"/"+filepath.Base(path)] = data
		}
	}

	return corpus
}

func TestRandomDamageAgainstTheReference(t *testing.T) {
	t.Parallel()

	flacBin, err := exec.LookPath("flac")
	if err != nil {
		t.Skip("no reference flac on the PATH")
	}

	cases := 200
	if s := os.Getenv("DAMAGE_CASES"); s != "" {
		cases, _ = strconv.Atoi(s)
	}

	corpus := damageCorpus(t, flacBin)

	names := make([]string, 0, len(corpus))
	for name := range corpus {
		names = append(names, name)
	}

	// Sorted for a reproducible walk under one seed.
	slices.Sort(names)

	rng := rand.New(rand.NewPCG(20261023, uint64(cases)))
	dir := t.TempDir()

	var bothOK, refOnly, oursOnly, bothRefuse int

	for i := range cases {
		name := names[rng.IntN(len(names))]
		data, what := damage(rng, corpus[name])

		// On disk before decoding: a panic in the decoder takes the process
		// with it, and the input must survive for the report.
		if err := os.WriteFile(filepath.Join(keepDir(t), "damage-current.flac"), data, 0o600); err != nil {
			t.Fatal(err)
		}

		ours, finished := decodeWithDeadline(data, 20*time.Second)
		if !finished {
			path := saveCase(t, i, data)
			t.Fatalf("case %d (%s, %s): the decoder did not finish in 20 s; input at %s", i, name, what, path)
		}

		path := filepath.Join(dir, "case.flac")
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}

		// The reference's decision is `flac -t`; its PCM is compared only at
		// the depths it writes raw (8, 16, 24, 32).
		refErr := exec.CommandContext(t.Context(), flacBin, "-t", "-s", path).Run()

		var reference []byte

		rawable := ours.err == nil && refErr == nil && rawDepth(ours.format)
		if rawable {
			var rawErr error

			reference, rawErr = flacBinaryDecodeRaw(flacBin, path)
			if rawErr != nil {
				t.Fatalf(
					"case %d (%s, %s): the reference accepts the stream but will not write it raw: %v",
					i,
					name,
					what,
					rawErr,
				)
			}
		}

		switch {
		case ours.err == nil && refErr == nil:
			bothOK++

			if rawable && !bytes.Equal(ours.pcm, reference) {
				saved := saveCase(t, i, data)
				t.Fatalf(
					"case %d (%s, %s): both decode, the PCM differs (%d bytes against the reference's %d); input at %s",
					i,
					name,
					what,
					len(ours.pcm),
					len(reference),
					saved,
				)
			}
		case ours.err == nil:
			oursOnly++

			t.Logf("case %d (%s, %s): decoded %d bytes where the reference refuses", i, name, what, len(ours.pcm))
		case refErr == nil:
			refOnly++

			if !errors.Is(ours.err, flac.ErrReadFailure) && !errors.Is(ours.err, flac.ErrBitDepth) {
				saved := saveCase(t, i, data)
				t.Fatalf(
					"case %d (%s, %s): refused outside the contract: %v; input at %s",
					i,
					name,
					what,
					ours.err,
					saved,
				)
			}

			t.Logf("case %d (%s, %s): refused (%v) where the reference decodes", i, name, what, ours.err)
		default:
			bothRefuse++

			if !errors.Is(ours.err, flac.ErrReadFailure) && !errors.Is(ours.err, flac.ErrBitDepth) {
				saved := saveCase(t, i, data)
				t.Fatalf(
					"case %d (%s, %s): refused outside the contract: %v; input at %s",
					i,
					name,
					what,
					ours.err,
					saved,
				)
			}
		}
	}

	t.Logf("%d cases: both decode %d, only the reference %d, only this decoder %d, both refuse %d",
		cases, bothOK, refOnly, oursOnly, bothRefuse)
}

// keepDir is where inputs worth keeping go: DAMAGE_KEEP when set, which
// outlives the test, else the test's own directory.
func keepDir(t *testing.T) string {
	t.Helper()

	if dir := os.Getenv("DAMAGE_KEEP"); dir != "" {
		return dir
	}

	return t.TempDir()
}

func saveCase(t *testing.T, i int, data []byte) string {
	t.Helper()

	path := filepath.Join(keepDir(t), fmt.Sprintf("damage-%d.flac", i))
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}
