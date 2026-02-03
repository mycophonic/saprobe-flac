package tests_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mycophonic/agar/pkg/agar"

	flac "github.com/mycophonic/saprobe-flac"
)

// decodeSaprobe decodes a FLAC file using the saprobe decoder.
func decodeSaprobe(path string) ([]byte, flac.PCMFormat, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, flac.PCMFormat{}, err
	}
	defer f.Close()

	return flac.Decode(f)
}

// encodeSaprobe encodes raw PCM to FLAC using saprobe's encoder.
func encodeSaprobe(srcPCM []byte, dstPath string, bitDepth, sampleRate, channels int) error {
	format := flac.PCMFormat{
		SampleRate: sampleRate,
		BitDepth:   flac.BitDepth(bitDepth),
		Channels:   uint(channels), //nolint:gosec // channels is 1-8.
	}

	f, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer f.Close()

	return flac.Encode(f, srcPCM, format)
}

// flacBinaryEncode encodes raw PCM to FLAC using the standalone flac binary.
func flacBinaryEncode(flacBin, srcPath, dstPath string, bitDepth, sampleRate, channels int) error {
	cmd := exec.Command(flacBin,
		"-f",
		"--force-raw-format",
		"--sign=signed",
		"--endian=little",
		fmt.Sprintf("--channels=%d", channels),
		fmt.Sprintf("--bps=%d", bitDepth),
		fmt.Sprintf("--sample-rate=%d", sampleRate),
		"-o", dstPath,
		srcPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("flac encode: %w\n%s", err, output)
	}

	return nil
}

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

// discoverFiles returns all .flac files in the given directory, sorted by name.
func discoverFiles(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("reading %s: %v", dir, err)
	}

	var files []string

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		if strings.HasSuffix(entry.Name(), ".flac") {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}

	if len(files) == 0 {
		t.Fatalf("no .flac files found in %s", dir)
	}

	return files
}

// Benchmark infrastructure.

const (
	benchIterations = agar.DefaultBenchIterations
	benchDuration   = 10 // seconds of audio
)

// benchResult wraps agar.BenchResult with a string-based Format for simpler test code.
type benchResult struct {
	Format  string
	Tool    string
	Op      string
	Median  time.Duration
	Mean    time.Duration
	Min     time.Duration
	Max     time.Duration
	Stddev  time.Duration
	PCMSize int
}

func computeResult(format, tool, op string, durations []time.Duration, pcmSize int) benchResult {
	r := agar.ComputeResult(agar.BenchFormat{Name: format}, tool, op, durations, pcmSize)

	return benchResult{
		Format:  format,
		Tool:    r.Tool,
		Op:      r.Op,
		Median:  r.Median,
		Mean:    r.Mean,
		Min:     r.Min,
		Max:     r.Max,
		Stddev:  r.Stddev,
		PCMSize: r.PCMSize,
	}
}

func printResults(t *testing.T, results []benchResult) {
	t.Helper()

	agarResults := make([]agar.BenchResult, len(results))
	for i, r := range results {
		agarResults[i] = agar.BenchResult{
			Format:  agar.BenchFormat{Name: r.Format},
			Tool:    r.Tool,
			Op:      r.Op,
			Median:  r.Median,
			Mean:    r.Mean,
			Min:     r.Min,
			Max:     r.Max,
			Stddev:  r.Stddev,
			PCMSize: r.PCMSize,
		}
	}

	agar.PrintResults(t, agar.BenchOptions{Iterations: benchIterations}, agarResults)
}
