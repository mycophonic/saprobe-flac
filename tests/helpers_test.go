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
	"fmt"
	"os/exec"

	"github.com/mycophonic/agar/pkg/agar"
)

// flacBinaryDecodeRaw decodes a FLAC file to raw PCM using the standalone flac binary.
func flacBinaryDecodeRaw(flacBin, srcPath string) ([]byte, error) {
	cmd := exec.Command(flacBin,
		"-d", "-f",
		"--force-raw-format",
		"--sign=signed",
		"--endian=little",
		"-o", "-",
		srcPath,
	)

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("flac decode: %w\n%s", err, stderr.String())
	}

	return stdout.Bytes(), nil
}

// Benchmark infrastructure.

var benchOpts = agar.BenchOptions{}.WithDefaults() //nolint:gochecknoglobals // shared benchmark options, read-only

const benchDuration = 10 // seconds of audio
