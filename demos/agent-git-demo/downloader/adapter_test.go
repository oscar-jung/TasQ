package downloader

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type fakeRunner struct {
	stdout   []byte
	stderr   []byte
	exitCode int
	err      error
}

func (f fakeRunner) Run(context.Context, string, []string) ([]byte, []byte, int, error) {
	return f.stdout, f.stderr, f.exitCode, f.err
}

func TestBuildArgsVideoDefaults(t *testing.T) {
	t.Parallel()

	request := Request{
		URL:       "https://example.test/watch?v=1",
		Mode:      ModeVideo,
		OutputDir: "/tmp/downloads",
	}

	got := BuildArgs(request)
	want := []string{
		"--no-progress",
		"--newline",
		"--print", "after_move:filepath",
		"--print", "format_id",
		"--output", "/tmp/downloads/%(title)s.%(ext)s",
		"-f", "best",
		"https://example.test/watch?v=1",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgs() mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestBuildArgsAudioExtraction(t *testing.T) {
	t.Parallel()

	request := Request{
		URL:             "https://example.test/watch?v=2",
		Mode:            ModeAudio,
		OutputDir:       "/tmp/downloads",
		QualitySelector: "251",
		ExtractAudio:    true,
		AudioFormat:     "mp3",
	}

	got := BuildArgs(request)
	want := []string{
		"--no-progress",
		"--newline",
		"--print", "after_move:filepath",
		"--print", "format_id",
		"--output", "/tmp/downloads/%(title)s.%(ext)s",
		"-f", "251",
		"-x",
		"--audio-format", "mp3",
		"https://example.test/watch?v=2",
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("BuildArgs() mismatch\n got: %#v\nwant: %#v", got, want)
	}
}

func TestDownloadSuccess(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	outputPath := filepath.Join(tempDir, "video.mp4")
	if err := os.WriteFile(outputPath, []byte("ok"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	adapter := NewAdapter().
		WithBackendBinary("/usr/local/bin/yt-dlp").
		WithRunner(fakeRunner{
			stdout: []byte(outputPath + "\n137\n"),
		})

	result, err := adapter.Download(context.Background(), Request{
		URL:       "https://example.test/watch?v=3",
		Mode:      ModeVideo,
		OutputDir: tempDir,
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}

	if result.OutputPath != outputPath {
		t.Fatalf("OutputPath = %q, want %q", result.OutputPath, outputPath)
	}
	if result.FormatID != "137" {
		t.Fatalf("FormatID = %q, want %q", result.FormatID, "137")
	}
	if result.BackendPath != "/usr/local/bin/yt-dlp" {
		t.Fatalf("BackendPath = %q, want %q", result.BackendPath, "/usr/local/bin/yt-dlp")
	}
}

func TestDownloadMapsBackendMissing(t *testing.T) {
	t.Parallel()

	adapter := NewAdapter().WithLookPath(func(string) (string, error) {
		return "", errors.New("missing")
	})

	_, err := adapter.Download(context.Background(), Request{
		URL:       "https://example.test/watch?v=4",
		Mode:      ModeVideo,
		OutputDir: t.TempDir(),
	})
	if !errors.Is(err, ErrBackendMissing) {
		t.Fatalf("Download() error = %v, want ErrBackendMissing", err)
	}
}

func TestDownloadMapsFFmpegMissing(t *testing.T) {
	t.Parallel()

	adapter := NewAdapter().
		WithBackendBinary("/usr/local/bin/yt-dlp").
		WithLookPath(func(binary string) (string, error) {
			if binary == "ffmpeg" {
				return "", errors.New("missing")
			}
			return "/usr/local/bin/" + binary, nil
		})

	_, err := adapter.Download(context.Background(), Request{
		URL:          "https://example.test/watch?v=5",
		Mode:         ModeAudio,
		OutputDir:    t.TempDir(),
		ExtractAudio: true,
		AudioFormat:  "mp3",
	})
	if !errors.Is(err, ErrFFmpegMissing) {
		t.Fatalf("Download() error = %v, want ErrFFmpegMissing", err)
	}
}

func TestDownloadMapsQualityUnavailable(t *testing.T) {
	t.Parallel()

	adapter := NewAdapter().
		WithBackendBinary("/usr/local/bin/yt-dlp").
		WithRunner(fakeRunner{
			stderr:   []byte("Requested format is not available"),
			exitCode: 1,
		})

	_, err := adapter.Download(context.Background(), Request{
		URL:             "https://example.test/watch?v=6",
		Mode:            ModeVideo,
		OutputDir:       t.TempDir(),
		QualitySelector: "999",
	})
	if !errors.Is(err, ErrQualityUnavailable) {
		t.Fatalf("Download() error = %v, want ErrQualityUnavailable", err)
	}
}

func TestDownloadRequiresReportedOutputFile(t *testing.T) {
	t.Parallel()

	adapter := NewAdapter().
		WithBackendBinary("/usr/local/bin/yt-dlp").
		WithRunner(fakeRunner{
			stdout: []byte("/tmp/missing.mp4\n22\n"),
		})

	_, err := adapter.Download(context.Background(), Request{
		URL:       "https://example.test/watch?v=7",
		Mode:      ModeVideo,
		OutputDir: t.TempDir(),
	})
	if !errors.Is(err, ErrOutputMissing) {
		t.Fatalf("Download() error = %v, want ErrOutputMissing", err)
	}
}
