package tests_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	flac "github.com/mycophonic/saprobe-flac"
)

// decodeFlacFile decodes a FLAC file using the saprobe decoder.
func decodeFlacFile(path string) ([]byte, flac.PCMFormat, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, flac.PCMFormat{}, err
	}
	defer f.Close()

	return flac.Decode(f)
}

// saprobeFlacEncode encodes raw PCM to FLAC using saprobe's encoder.
func saprobeFlacEncode(srcPCM []byte, dstPath string, bitDepth, sampleRate, channels int) error {
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

// generateWhiteNoise creates random PCM data.
func generateWhiteNoise(sampleRate, bitDepth, channels, durationSec int) []byte {
	numSamples := sampleRate * durationSec * channels
	bytesPerSample := pcmBytesPerSample(bitDepth)

	buf := make([]byte, numSamples*bytesPerSample)

	// Use a simple PRNG for reproducibility.
	seed := uint64(0x12345678)

	for i := range numSamples {
		// xorshift64
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17

		offset := i * bytesPerSample

		switch bitDepth {
		case 4:
			buf[offset] = byte(int8((seed % 14) - 7))
		case 8:
			buf[offset] = byte(int8((seed % 240) - 120))
		case 12:
			val := int16((seed % 4000) - 2000)
			binary.LittleEndian.PutUint16(buf[offset:], uint16(val))
		case 16:
			val := int16((seed % 60000) - 30000)
			binary.LittleEndian.PutUint16(buf[offset:], uint16(val))
		case 20:
			val := int32((seed % 1000000) - 500000)
			buf[offset] = byte(val)
			buf[offset+1] = byte(val >> 8)
			buf[offset+2] = byte(val >> 16)
		case 24:
			val := int32((seed % 14000000) - 7000000)
			buf[offset] = byte(val)
			buf[offset+1] = byte(val >> 8)
			buf[offset+2] = byte(val >> 16)
		case 32:
			val := int32((seed % 1800000000) - 900000000)
			binary.LittleEndian.PutUint32(buf[offset:], uint32(val))

		default:
		}
	}

	return buf
}

// compareLosslessSamples requires exact byte match for lossless codecs.
func compareLosslessSamples(t *testing.T, label string, expected, actual []byte, bitDepth, channels int) {
	t.Helper()

	minLen := min(len(expected), len(actual))
	differences := 0
	firstDiff := -1

	for i := range minLen {
		if expected[i] != actual[i] {
			differences++

			if firstDiff == -1 {
				firstDiff = i
			}
		}
	}

	if differences > 0 {
		bytesPerSample := pcmBytesPerSample(bitDepth)
		sampleIndex := firstDiff / bytesPerSample / channels
		t.Errorf("%s: PCM mismatch: %d differing bytes (%.2f%%), first diff at byte %d (sample %d)",
			label, differences, float64(differences)/float64(minLen)*100, firstDiff, sampleIndex)

		showDiffs(t, label, expected, actual, bitDepth, channels, 5)
	}
}

// pcmBytesPerSample returns the number of bytes per sample for a given bit depth.
func pcmBytesPerSample(bitDepth int) int {
	switch bitDepth {
	case 4, 8:
		return 1
	case 12, 16:
		return 2
	case 20, 24:
		return 3
	case 32:
		return 4
	default:
		return bitDepth / 8
	}
}

// showDiffs prints the first N differing samples for debugging.
func showDiffs(t *testing.T, label string, expected, actual []byte, bitDepth, channels, maxDiffs int) {
	t.Helper()

	bytesPerSample := pcmBytesPerSample(bitDepth)
	frameSize := bytesPerSample * channels
	shown := 0

	for i := 0; i < min(len(expected), len(actual))-frameSize && shown < maxDiffs; i += frameSize {
		expectedFrame := expected[i : i+frameSize]
		actualFrame := actual[i : i+frameSize]

		if !bytes.Equal(expectedFrame, actualFrame) {
			sampleIdx := i / frameSize
			t.Logf("%s: sample %d: expected=%v, actual=%v", label, sampleIdx, expectedFrame, actualFrame)

			shown++
		}
	}
}

// projectRoot returns the project root (parent of tests/).
func projectRoot(t *testing.T) string {
	t.Helper()

	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}

	for {
		if _, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil {
			return dir
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find project root (go.mod)")
		}

		dir = parent
	}
}

// discoverFlacFiles returns all .flac files in the given directory, sorted by name.
func discoverFlacFiles(t *testing.T, dir string) []string {
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
	benchIterations = 10
	benchDuration   = 10 // seconds of audio
)

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
	sorted := make([]time.Duration, len(durations))
	copy(sorted, durations)
	slices.Sort(sorted)

	var sum float64
	for _, d := range durations {
		sum += float64(d)
	}

	mean := sum / float64(len(durations))

	var variance float64

	for _, d := range durations {
		diff := float64(d) - mean
		variance += diff * diff
	}

	variance /= float64(len(durations))

	return benchResult{
		Format:  format,
		Tool:    tool,
		Op:      op,
		Median:  sorted[len(sorted)/2],
		Mean:    time.Duration(mean),
		Min:     sorted[0],
		Max:     sorted[len(sorted)-1],
		Stddev:  time.Duration(math.Sqrt(variance)),
		PCMSize: pcmSize,
	}
}

func printResults(t *testing.T, results []benchResult) {
	t.Helper()

	sep := "──────────────────────────────────────────────────────────────────"

	t.Log("")
	t.Log("┌" + sep + "┐")
	t.Logf("│ FLAC Benchmark Results (%d iterations per test)%s│",
		benchIterations, "                  ")
	t.Log("├" + sep + "┤")
	t.Logf("│ %-24s %-7s %-6s %8s %8s %8s %8s│",
		"Format", "Tool", "Op", "Median", "Mean", "Min", "Max")
	t.Log("├" + sep + "┤")

	currentFormat := ""

	for _, r := range results {
		if r.Format != currentFormat {
			if currentFormat != "" {
				t.Log("├" + sep + "┤")
			}

			currentFormat = r.Format
		}

		t.Logf("│ %-24s %-7s %-6s %8s %8s %8s %8s│",
			r.Format, r.Tool, r.Op,
			r.Median.Round(time.Millisecond),
			r.Mean.Round(time.Millisecond),
			r.Min.Round(time.Millisecond),
			r.Max.Round(time.Millisecond),
		)
	}

	t.Log("└" + sep + "┘")
}

func fileSize(t *testing.T, path string) int {
	t.Helper()

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	return int(info.Size())
}
