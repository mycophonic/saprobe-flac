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
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/mycophonic/agar/pkg/agar"

	flac "github.com/mycophonic/saprobe-flac"
)

type benchFormat struct {
	Name       string
	SampleRate int
	BitDepth   int
	Channels   int
}

//nolint:gochecknoglobals
var benchFormats = []benchFormat{
	{"CD 44.1kHz/16bit", 44100, 16, 2},
	{"HiRes 96kHz/24bit", 96000, 24, 2},
	{"UltraHiRes 192kHz/24bit", 192000, 24, 2},
	{"Studio 192kHz/32bit", 192000, 32, 2},
}

//nolint:paralleltest // Benchmark must run sequentially for accurate timing.
func TestBenchmarkEncode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmark in short mode")
	}

	flacBin, flacBinErr := agar.LookFor("flac")
	if flacBinErr != nil {
		t.Skip("flac binary not found")
	}

	tmpDir := t.TempDir()

	var results []benchResult

	for _, bf := range benchFormats {
		t.Logf("=== %s ===", bf.Name)

		srcPCM := agar.GenerateWhiteNoise(bf.SampleRate, bf.BitDepth, bf.Channels, benchDuration)
		srcPath := filepath.Join(tmpDir, fmt.Sprintf("src_%d_%d.raw", bf.SampleRate, bf.BitDepth))

		if err := os.WriteFile(srcPath, srcPCM, 0o600); err != nil {
			t.Fatalf("write source: %v", err)
		}

		t.Logf("  PCM size: %.1f MB (%d bytes)", float64(len(srcPCM))/(1024*1024), len(srcPCM))

		dstSaprobe := filepath.Join(tmpDir, fmt.Sprintf("enc_saprobe_%d_%d.flac", bf.SampleRate, bf.BitDepth))
		results = append(results, benchEncodeSaprobe(t, bf, srcPCM, dstSaprobe))

		dstFlac := filepath.Join(tmpDir, fmt.Sprintf("enc_flac_%d_%d.flac", bf.SampleRate, bf.BitDepth))
		results = append(results, benchEncodeFlacBin(t, bf, flacBin, srcPath, dstFlac))

		if bf.BitDepth == 16 || bf.BitDepth == 24 {
			dstFFmpeg := filepath.Join(tmpDir, fmt.Sprintf("enc_ffmpeg_%d_%d.flac", bf.SampleRate, bf.BitDepth))
			results = append(results, benchEncodeFFmpeg(t, bf, srcPath, dstFFmpeg))
		}
	}

	printResults(t, results)
}

//nolint:paralleltest // Benchmark must run sequentially for accurate timing.
func TestBenchmarkDecode(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmark in short mode")
	}

	flacBin, flacBinErr := agar.LookFor("flac")
	if flacBinErr != nil {
		t.Skip("flac binary not found")
	}

	tmpDir := t.TempDir()

	var results []benchResult

	for _, bf := range benchFormats {
		t.Logf("=== %s ===", bf.Name)

		srcPCM := agar.GenerateWhiteNoise(bf.SampleRate, bf.BitDepth, bf.Channels, benchDuration)
		encPath := filepath.Join(tmpDir, fmt.Sprintf("enc_%d_%d.flac", bf.SampleRate, bf.BitDepth))

		if err := encodeForBench(srcPCM, encPath, bf); err != nil {
			t.Fatalf("encode setup: %v", err)
		}

		t.Logf("  PCM size: %.1f MB (%d bytes)", float64(len(srcPCM))/(1024*1024), len(srcPCM))

		results = append(results, benchDecodeSaprobe(t, bf, encPath))
		results = append(results, benchDecodeFlacBin(t, bf, flacBin, encPath))
		results = append(results, benchDecodeFFmpeg(t, bf, encPath))
		results = append(results, benchDecodeCoreAudio(t, bf, encPath))
	}

	printResults(t, results)
}

// encodeForBench encodes raw PCM to FLAC once (no timing), used as setup for decode benchmarks.
func encodeForBench(srcPCM []byte, dstPath string, bf benchFormat) error {
	format := flac.PCMFormat{
		SampleRate: bf.SampleRate,
		BitDepth:   flac.BitDepth(bf.BitDepth),
		Channels:   uint(bf.Channels),
	}

	var buf bytes.Buffer
	if err := flac.Encode(&buf, srcPCM, format); err != nil {
		return err
	}

	return os.WriteFile(dstPath, buf.Bytes(), 0o600)
}

func benchEncodeSaprobe(t *testing.T, bf benchFormat, srcPCM []byte, dstPath string) benchResult {
	t.Helper()

	format := flac.PCMFormat{
		SampleRate: bf.SampleRate,
		BitDepth:   flac.BitDepth(bf.BitDepth),
		Channels:   uint(bf.Channels),
	}

	durations := make([]time.Duration, benchIterations)

	for iter := range benchIterations {
		var buf bytes.Buffer

		start := time.Now()

		if err := flac.Encode(&buf, srcPCM, format); err != nil {
			t.Fatalf("saprobe encode: %v", err)
		}

		durations[iter] = time.Since(start)

		// Write the last iteration to disk for decode benchmarks.
		if iter == benchIterations-1 {
			if err := os.WriteFile(dstPath, buf.Bytes(), 0o600); err != nil {
				t.Fatalf("write encoded: %v", err)
			}

			ratio := float64(buf.Len()) / float64(len(srcPCM)) * 100
			t.Logf("  saprobe encode: %.1f%% ratio (%d bytes)", ratio, buf.Len())
		}
	}

	return computeResult(bf.Name, "saprobe", "encode", durations, len(srcPCM))
}

func benchEncodeFlacBin(t *testing.T, bf benchFormat, flacBin, srcPath, dstPath string) benchResult {
	t.Helper()

	durations := make([]time.Duration, benchIterations)

	for iter := range benchIterations {
		start := time.Now()

		cmd := exec.Command(flacBin,
			"-f", "--silent",
			"--force-raw-format",
			"--sign=signed",
			"--endian=little",
			fmt.Sprintf("--channels=%d", bf.Channels),
			fmt.Sprintf("--bps=%d", bf.BitDepth),
			fmt.Sprintf("--sample-rate=%d", bf.SampleRate),
			"-o", dstPath,
			srcPath,
		)

		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("flac encode: %v\n%s", err, output)
		}

		durations[iter] = time.Since(start)
	}

	return computeResult(bf.Name, "flac", "encode", durations, agar.FileSize(t, srcPath))
}

func benchEncodeFFmpeg(t *testing.T, bf benchFormat, srcPath, dstPath string) benchResult {
	t.Helper()

	var sampleFmt string

	switch bf.BitDepth {
	case 16:
		sampleFmt = "s16"
	case 24:
		sampleFmt = "s32"
	default:
		t.Fatalf("ffmpeg encode: unsupported bit depth %d", bf.BitDepth)
	}

	durations := make([]time.Duration, benchIterations)

	for iter := range benchIterations {
		start := time.Now()

		agar.FFmpegEncode(t, agar.FFmpegEncodeOptions{
			Src: srcPath, Dst: dstPath,
			BitDepth: bf.BitDepth, SampleRate: bf.SampleRate, Channels: bf.Channels,
			CodecArgs: []string{"-c:a", "flac", "-sample_fmt", sampleFmt},
		})

		durations[iter] = time.Since(start)
	}

	return computeResult(bf.Name, "ffmpeg", "encode", durations, agar.FileSize(t, srcPath))
}

func benchDecodeSaprobe(t *testing.T, bf benchFormat, srcPath string) benchResult {
	t.Helper()

	encoded, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read encoded: %v", err)
	}

	durations := make([]time.Duration, benchIterations)

	for iter := range benchIterations {
		start := time.Now()

		_, _, err := flac.Decode(bytes.NewReader(encoded))
		if err != nil {
			t.Fatalf("saprobe decode: %v", err)
		}

		durations[iter] = time.Since(start)
	}

	return computeResult(bf.Name, "saprobe", "decode", durations, len(encoded))
}

func benchDecodeFlacBin(t *testing.T, bf benchFormat, flacBin, srcPath string) benchResult {
	t.Helper()

	durations := make([]time.Duration, benchIterations)

	for iter := range benchIterations {
		start := time.Now()

		cmd := exec.Command(flacBin,
			"-d", "-f", "--silent",
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
			t.Fatalf("flac decode: %v\n%s", err, stderr.String())
		}

		durations[iter] = time.Since(start)
	}

	return computeResult(bf.Name, "flac", "decode", durations, agar.FileSize(t, srcPath))
}

func benchDecodeFFmpeg(t *testing.T, bf benchFormat, srcPath string) benchResult {
	t.Helper()

	durations := make([]time.Duration, benchIterations)

	for iter := range benchIterations {
		start := time.Now()

		agar.FFmpegDecode(t, agar.FFmpegDecodeOptions{
			Src: srcPath, BitDepth: bf.BitDepth, Channels: bf.Channels,
			Stdout: io.Discard,
		})

		durations[iter] = time.Since(start)
	}

	return computeResult(bf.Name, "ffmpeg", "decode", durations, agar.FileSize(t, srcPath))
}

func benchDecodeCoreAudio(t *testing.T, bf benchFormat, srcPath string) benchResult {
	t.Helper()

	encoded, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read encoded: %v", err)
	}

	// Verify CoreAudio decode works before benchmarking.
	// CoreAudio may not support all formats (e.g. 32-bit FLAC).
	if _, err := agar.CoreAudioDecode(encoded); err != nil {
		t.Logf("coreaudio: %v", err)

		return benchResult{}
	}

	durations := make([]time.Duration, benchIterations)

	for iter := range benchIterations {
		start := time.Now()

		if _, err := agar.CoreAudioDecode(encoded); err != nil {
			t.Fatalf("coreaudio decode iter %d: %v", iter, err)
		}

		durations[iter] = time.Since(start)
	}

	return computeResult(bf.Name, "coreaudio", "decode", durations, len(encoded))
}

//nolint:paralleltest // Benchmark must run sequentially for accurate timing.
func TestBenchmarkDecodeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping benchmark in short mode")
	}

	filePath := os.Getenv("BENCH_FLAC_FILE")
	if filePath == "" {
		t.Skip("set BENCH_FLAC_FILE to run this benchmark")
	}

	encoded, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}

	t.Logf("File: %s (%.1f MB)", filePath, float64(len(encoded))/(1024*1024))

	flacBin, flacBinErr := agar.LookFor("flac")

	var results []benchResult

	bf := benchFormat{Name: filepath.Base(filePath), Channels: 2}

	// saprobe decode
	durations := make([]time.Duration, benchIterations)

	for iter := range benchIterations {
		start := time.Now()

		_, _, decErr := flac.Decode(bytes.NewReader(encoded))
		if decErr != nil {
			t.Fatalf("saprobe decode: %v", decErr)
		}

		durations[iter] = time.Since(start)
	}

	results = append(results, computeResult(bf.Name, "saprobe", "decode", durations, len(encoded)))

	// Write to temp for tool-based decoders.
	tmpFile := filepath.Join(t.TempDir(), "input.flac")
	if writeErr := os.WriteFile(tmpFile, encoded, 0o600); writeErr != nil {
		t.Fatalf("write temp: %v", writeErr)
	}

	// flac binary decode
	if flacBinErr == nil {
		results = append(results, benchDecodeFlacBin(t, bf, flacBin, tmpFile))
	}

	results = append(results, benchDecodeFFmpeg(t, bf, tmpFile))
	results = append(results, benchDecodeCoreAudio(t, bf, tmpFile))

	printResults(t, results)
}
