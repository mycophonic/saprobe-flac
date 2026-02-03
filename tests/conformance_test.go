package tests_test

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/mycophonic/agar/pkg/agar"
)

//nolint:gochecknoglobals
var conformanceRepo = flag.String(
	"conformance-repo", "",
	"path to ietf-wg-cellar/flac-test-files clone (auto-downloaded if empty)",
)

const conformanceRepoURL = "https://github.com/ietf-wg-cellar/flac-test-files.git"

// TestIETFConformance runs the IETF FLAC decoder testbench against three decoders:
// the reference flac binary, ffmpeg, and saprobe. Subset files must decode identically
// across all three. Uncommon files are best-effort. Faulty files must not crash.
func TestIETFConformance(t *testing.T) {
	t.Parallel()

	repoPath := ensureConformanceRepo(t)

	flacBin, flacBinErr := agar.LookFor("flac")
	if flacBinErr != nil {
		t.Skip("standalone flac binary not found")
	}

	_, ffmpegBinErr := agar.LookFor("ffmpeg")

	t.Run("subset", func(t *testing.T) {
		t.Parallel()

		files := discoverFiles(t, filepath.Join(repoPath, "subset"))
		for _, path := range files {
			t.Run(filepath.Base(path), func(t *testing.T) {
				t.Parallel()

				props := probeFile(t, path)

				runSubsetTest(t, path, props, flacBin, ffmpegBinErr)
			})
		}
	})

	t.Run("uncommon", func(t *testing.T) {
		t.Parallel()

		files := discoverFiles(t, filepath.Join(repoPath, "uncommon"))
		for _, path := range files {
			t.Run(filepath.Base(path), func(t *testing.T) {
				t.Parallel()

				runUncommonTest(t, path, flacBin)
			})
		}
	})

	t.Run("faulty", func(t *testing.T) {
		t.Parallel()

		files := discoverFiles(t, filepath.Join(repoPath, "faulty"))
		for _, path := range files {
			t.Run(filepath.Base(path), func(t *testing.T) {
				t.Parallel()

				runFaultyTest(t, path)
			})
		}
	})
}

// conformanceProps holds ffprobe-derived properties for a conformance test file.
type conformanceProps struct {
	bitDepth int
	channels int
}

func probeFile(t *testing.T, path string) conformanceProps {
	t.Helper()

	result, err := agar.FFProbe(path)
	if err != nil {
		t.Fatalf("ffprobe %s: %v", filepath.Base(path), err)
	}

	stream, streamErr := result.AudioStream()
	if streamErr != nil {
		t.Fatalf("ffprobe %s: %v", filepath.Base(path), streamErr)
	}

	return conformanceProps{
		bitDepth: stream.BitDepth(),
		channels: stream.Channels,
	}
}

// flacBinRawSupported reports whether the reference flac binary can produce
// raw PCM output at the given bit depth.
func flacBinRawSupported(bitDepth int) bool {
	switch bitDepth {
	case 8, 16, 24, 32:
		return true
	default:
		return false
	}
}

// runSubsetTest decodes a subset FLAC file with all three decoders and compares output.
func runSubsetTest(
	t *testing.T,
	path string,
	props conformanceProps,
	flacBin string,
	ffmpegBinErr error,
) {
	t.Helper()

	saprobePCM, _, saprobeErr := decodeSaprobe(path)
	if saprobeErr != nil {
		t.Fatalf("saprobe decode: %v", saprobeErr)
	}

	var refPCM []byte

	if !flacBinRawSupported(props.bitDepth) {
		t.Logf("skipping flac binary comparison: --force-raw-format does not support %d-bit output", props.bitDepth)
	} else {
		var err error

		refPCM, err = flacBinaryDecodeRaw(flacBin, path)
		if err != nil {
			t.Fatalf("flac binary decode: %v", err)
		}

		if !bytes.Equal(saprobePCM, refPCM) {
			reportPCMDiff(t, "saprobe vs flac", saprobePCM, refPCM, props.bitDepth, props.channels)
		}
	}

	if ffmpegBinErr != nil {
		t.Log("skipping ffmpeg comparison: ffmpeg not found")

		return
	}

	if !flacBinRawSupported(props.bitDepth) {
		t.Logf("skipping ffmpeg comparison: no native raw PCM format for %d-bit output", props.bitDepth)

		return
	}

	if ffmpegMultichannelFails(props.bitDepth, props.channels) {
		t.Logf(
			"skipping ffmpeg comparison: known channel remapping for %d-bit/%dch",
			props.bitDepth, props.channels,
		)

		return
	}

	ffmpegPCM := agar.FFmpegDecode(
		t,
		agar.FFmpegDecodeOptions{Src: path, BitDepth: props.bitDepth, Channels: props.channels},
	)

	if !bytes.Equal(saprobePCM, ffmpegPCM) {
		reportPCMDiff(t, "saprobe vs ffmpeg", saprobePCM, ffmpegPCM, props.bitDepth, props.channels)
	}

	if refPCM != nil && !bytes.Equal(refPCM, ffmpegPCM) {
		reportPCMDiff(t, "flac vs ffmpeg", refPCM, ffmpegPCM, props.bitDepth, props.channels)
	}
}

// uncommonSkips lists uncommon FLAC test files that saprobe cannot decode yet.
//
//nolint:gochecknoglobals
var uncommonSkips = map[string]string{
	"10 - file starting at frame header.flac": "headerless FLAC (no fLaC signature) not supported",
}

func runUncommonTest(t *testing.T, path, flacBin string) {
	t.Helper()

	if reason, ok := uncommonSkips[filepath.Base(path)]; ok {
		t.Skipf("known unsupported: %s", reason)
	}

	refPCM, refErr := flacBinaryDecodeRaw(flacBin, path)

	var (
		saprobePCM []byte
		saprobeErr error
		didPanic   bool
		panicValue any
	)

	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
				panicValue = r
			}
		}()

		saprobePCM, _, saprobeErr = decodeSaprobe(path)
	}()

	if didPanic {
		t.Errorf("PANIC on uncommon file %s: %v", filepath.Base(path), panicValue)

		return
	}

	switch {
	case refErr != nil && saprobeErr != nil:
		t.Logf("both failed (acceptable): flac=%v, saprobe=%v", refErr, saprobeErr)
	case refErr != nil && saprobeErr == nil:
		t.Logf("flac binary failed but saprobe succeeded (%d bytes)", len(saprobePCM))
	case refErr == nil && saprobeErr != nil:
		t.Errorf("flac binary succeeded (%d bytes) but saprobe failed: %v", len(refPCM), saprobeErr)
	default:
		if !bytes.Equal(saprobePCM, refPCM) {
			t.Errorf(
				"saprobe vs flac mismatch: saprobe=%d bytes, flac=%d bytes",
				len(saprobePCM), len(refPCM),
			)
		}
	}
}

// runFaultyTest attempts to decode a faulty FLAC file with saprobe.
// The file is invalid; the test verifies saprobe doesn't crash (no panics).
func runFaultyTest(t *testing.T, path string) {
	t.Helper()

	var (
		decodeErr  error
		didPanic   bool
		panicValue any
	)

	func() {
		defer func() {
			if r := recover(); r != nil {
				didPanic = true
				panicValue = r
			}
		}()

		_, _, decodeErr = decodeSaprobe(path)
	}()

	switch {
	case didPanic:
		t.Errorf("PANIC on faulty file %s: %v", filepath.Base(path), panicValue)
	case decodeErr == nil:
		t.Logf("decoded without error (acceptable — no crash): %s", filepath.Base(path))
	default:
		t.Logf("correctly rejected: %v", decodeErr)
	}
}

// reportPCMDiff logs detailed information about a PCM mismatch.
func reportPCMDiff(t *testing.T, label string, got, want []byte, bitDepth, channels int) {
	t.Helper()

	if len(got) != len(want) {
		t.Errorf("%s: length mismatch: got=%d, want=%d", label, len(got), len(want))
	}

	minLen := min(len(got), len(want))
	firstDiff := -1

	for i := range minLen {
		if got[i] != want[i] {
			firstDiff = i

			break
		}
	}

	if firstDiff == -1 {
		if len(got) != len(want) {
			t.Errorf("%s: identical up to byte %d, then sizes differ", label, minLen)
		}

		return
	}

	bytesPerSample := agar.PCMBytesPerSample(bitDepth)
	frameSize := bytesPerSample * channels
	sampleIdx := firstDiff / frameSize

	t.Errorf(
		"%s: first diff at byte %d (sample %d), got=0x%02X want=0x%02X",
		label, firstDiff, sampleIdx, got[firstDiff], want[firstDiff],
	)

	diffCount := 0

	for i := range minLen {
		if got[i] != want[i] {
			diffCount++
		}
	}

	t.Errorf("%s: %d/%d bytes differ (%.2f%%)", label, diffCount, minLen, float64(diffCount)*100/float64(minLen))
}

// ensureConformanceRepo returns the path to the flac-test-files repository.
//
//nolint:cyclop // Sequential setup logic.
func ensureConformanceRepo(t *testing.T) string {
	t.Helper()

	if *conformanceRepo != "" {
		if _, err := os.Stat(*conformanceRepo); err != nil {
			t.Fatalf("conformance repo not found at %s: %v", *conformanceRepo, err)
		}

		return *conformanceRepo
	}

	repoDir := filepath.Join(agar.ProjectRoot(t), "bin", "flac-test-files")

	conformanceOnce.Do(func() {
		if info, err := os.Stat(filepath.Join(repoDir, "subset")); err == nil && info.IsDir() {
			return
		}

		gitBin, err := exec.LookPath("git")
		if err != nil {
			conformanceErr = fmt.Errorf("git not found: %w", err)

			return
		}

		t.Logf("cloning %s into %s", conformanceRepoURL, repoDir)

		if mkErr := os.MkdirAll(filepath.Dir(repoDir), 0o755); mkErr != nil {
			conformanceErr = fmt.Errorf("mkdir: %w", mkErr)

			return
		}

		cmd := exec.Command(gitBin, "clone", "--depth", "1", conformanceRepoURL, repoDir)
		cmd.Stderr = os.Stderr

		if cloneErr := cmd.Run(); cloneErr != nil {
			conformanceErr = fmt.Errorf("git clone: %w", cloneErr)
		}
	})

	if conformanceErr != nil {
		t.Skipf("could not download IETF FLAC test files: %v", conformanceErr)
	}

	return repoDir
}

//nolint:gochecknoglobals
var (
	conformanceOnce sync.Once
	conformanceErr  error
)
