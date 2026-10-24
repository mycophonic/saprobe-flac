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
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	flac "github.com/mycophonic/saprobe-flac"
)

// The example decoder as a program: built once, it must exit non-zero
// without a panic on no argument, a missing file, a file that is not FLAC,
// a stream cut inside a frame and an unknown format, and exit zero with a
// WAV on a good stream.
func TestExampleDecoderExitsCleanly(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	// Windows runs only what ends in .exe.
	bin := filepath.Join(dir, "decoder")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}

	build := exec.CommandContext(t.Context(), "go", "build", "-o", bin, "../examples/decoder")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}

	// Two thousand stereo frames of a tone.
	pcm := make([]byte, 0, 2000*4)

	for i := range 2000 {
		v := int16(12000 * math.Sin(float64(i)/9))
		pcm = append(pcm, byte(v), byte(v>>8), byte(-v), byte(-v>>8))
	}

	var good bytes.Buffer
	if err := flac.Encode(
		&good,
		pcm,
		flac.PCMFormat{SampleRate: 44100, BitDepth: flac.Depth16, Channels: 2},
	); err != nil {
		t.Fatal(err)
	}

	goodPath := filepath.Join(dir, "good.flac")
	cutPath := filepath.Join(dir, "cut.flac")
	notPath := filepath.Join(dir, "not.flac")

	for path, data := range map[string][]byte{
		goodPath: good.Bytes(),
		cutPath:  good.Bytes()[:good.Len()-9],
		notPath:  []byte("RIFF this is not FLAC at all, not even close"),
	} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	cases := []struct {
		name string
		args []string
		ok   bool
	}{
		{"no argument", nil, false},
		{"a missing file", []string{filepath.Join(dir, "missing.flac")}, false},
		{"not a FLAC file", []string{notPath}, false},
		{"a stream cut inside a frame", []string{cutPath}, false},
		{"an unknown format", []string{"-format", "ogg", goodPath}, false},
		{"a good stream to WAV", []string{"-format", "wav", goodPath}, true},
		{"a good stream to PCM", []string{"-format", "pcm", goodPath}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()

			cmd := exec.CommandContext(t.Context(), bin, c.args...)
			cmd.Dir = t.TempDir()

			var stdout, stderr bytes.Buffer

			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			err := cmd.Run()

			if strings.Contains(stderr.String(), "panic") || strings.Contains(stderr.String(), "goroutine ") {
				t.Fatalf("the program panicked:\n%s", stderr.String())
			}

			if (err == nil) != c.ok {
				t.Fatalf("exit error %v, want success %v; stderr:\n%s", err, c.ok, stderr.String())
			}

			if c.ok && stdout.Len() == 0 {
				t.Fatal("a good stream produced no output")
			}
		})
	}
}
