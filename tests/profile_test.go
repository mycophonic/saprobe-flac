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
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mycophonic/agar/pkg/agar"
)

// TestProfileEncode runs saprobe-only encoding for clean pprof profiling.
// Use with: hack/bench.sh TestProfileEncode
//
//nolint:paralleltest // Profile must run sequentially for accurate sampling.
func TestProfileEncode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping profile in short mode")
	}

	tmpDir := t.TempDir()

	var results []agar.BenchResult

	for _, bf := range benchFormats {
		t.Logf("=== %s ===", bf.Name)

		srcPCM := agar.GenerateWhiteNoise(bf.SampleRate, bf.BitDepth, bf.Channels, benchDuration)

		t.Logf("  PCM size: %.1f MB (%d bytes)", float64(len(srcPCM))/(1024*1024), len(srcPCM))

		dstPath := filepath.Join(tmpDir, fmt.Sprintf("prof_enc_%d_%d.flac", bf.SampleRate, bf.BitDepth))
		results = append(results, benchEncodeSaprobe(t, bf, srcPCM, dstPath))
	}

	agar.PrintResults(t, benchOpts, results)
}

// TestProfileDecode runs saprobe-only decoding for clean pprof profiling.
// Use with: hack/bench.sh TestProfileDecode
//
//nolint:paralleltest // Profile must run sequentially for accurate sampling.
func TestProfileDecode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping profile in short mode")
	}

	tmpDir := t.TempDir()

	var results []agar.BenchResult

	for _, bf := range benchFormats {
		t.Logf("=== %s ===", bf.Name)

		srcPCM := agar.GenerateWhiteNoise(bf.SampleRate, bf.BitDepth, bf.Channels, benchDuration)
		encPath := filepath.Join(tmpDir, fmt.Sprintf("prof_%d_%d.flac", bf.SampleRate, bf.BitDepth))

		if err := encodeForBench(srcPCM, encPath, bf); err != nil {
			t.Fatalf("encode setup: %v", err)
		}

		t.Logf("  PCM size: %.1f MB (%d bytes)", float64(len(srcPCM))/(1024*1024), len(srcPCM))

		results = append(results, benchDecodeSaprobe(t, bf, encPath))
	}

	agar.PrintResults(t, benchOpts, results)
}

// TestProfileDecodeFile runs saprobe-only decoding of a real file for clean pprof profiling.
// Set BENCH_FLAC_FILE to a FLAC file path.
// Use with: hack/bench.sh TestProfileDecodeFile /path/to/file.flac
//
//nolint:paralleltest // Profile must run sequentially for accurate sampling.
func TestProfileDecodeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping profile in short mode")
	}

	filePath := os.Getenv("BENCH_FLAC_FILE")
	if filePath == "" {
		t.Skip("set BENCH_FLAC_FILE to run this profile")
	}

	encoded, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	t.Logf("File: %s (%.1f MB)", filePath, float64(len(encoded))/(1024*1024))

	tmpFile := filepath.Join(t.TempDir(), "input.flac")
	if writeErr := os.WriteFile(tmpFile, encoded, 0o600); writeErr != nil {
		t.Fatalf("write temp: %v", writeErr)
	}

	bf := agar.BenchFormat{Name: filepath.Base(filePath), Channels: 2}

	results := []agar.BenchResult{benchDecodeSaprobe(t, bf, tmpFile)}

	agar.PrintResults(t, benchOpts, results)
}
