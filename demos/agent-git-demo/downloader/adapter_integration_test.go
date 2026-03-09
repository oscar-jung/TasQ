package downloader

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAdapterIntegrationVideoDownload(t *testing.T) {
	t.Parallel()

	url := requireIntegrationURL(t)
	adapter := integrationAdapter(t)
	outputDir := t.TempDir()

	result, err := runIntegrationDownload(t, adapter, Request{
		URL:              url,
		Mode:             ModeVideo,
		OutputDir:        outputDir,
		FilenameTemplate: "video-%(id)s.%(ext)s",
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if result.OutputPath == "" {
		t.Fatal("OutputPath is empty")
	}
	if _, err := os.Stat(result.OutputPath); err != nil {
		t.Fatalf("downloaded file missing: %v", err)
	}
}

func TestAdapterIntegrationAudioDownload(t *testing.T) {
	t.Parallel()

	url := requireIntegrationURL(t)
	adapter := integrationAdapter(t)
	outputDir := t.TempDir()

	result, err := runIntegrationDownload(t, adapter, Request{
		URL:              url,
		Mode:             ModeAudio,
		OutputDir:        outputDir,
		FilenameTemplate: "audio-%(id)s.%(ext)s",
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if result.RequestedMode != ModeAudio {
		t.Fatalf("RequestedMode = %q, want %q", result.RequestedMode, ModeAudio)
	}
	if _, err := os.Stat(result.OutputPath); err != nil {
		t.Fatalf("downloaded file missing: %v", err)
	}
}

func TestAdapterIntegrationQualitySelection(t *testing.T) {
	t.Parallel()

	url := requireIntegrationURL(t)
	selector := strings.TrimSpace(os.Getenv("TEST_QUALITY_SELECTOR"))
	if selector == "" {
		selector = "best"
	}

	adapter := integrationAdapter(t)
	result, err := runIntegrationDownload(t, adapter, Request{
		URL:              url,
		Mode:             ModeVideo,
		OutputDir:        t.TempDir(),
		FilenameTemplate: "quality-%(id)s.%(ext)s",
		QualitySelector:  selector,
	})
	if err != nil {
		t.Fatalf("Download() error = %v", err)
	}
	if result.FormatID == "" {
		t.Fatal("FormatID is empty")
	}
}

func TestAdapterIntegrationUnavailableQuality(t *testing.T) {
	t.Parallel()

	url := requireIntegrationURL(t)
	adapter := integrationAdapter(t)

	_, err := runIntegrationDownload(t, adapter, Request{
		URL:              url,
		Mode:             ModeVideo,
		OutputDir:        t.TempDir(),
		FilenameTemplate: "failure-%(id)s.%(ext)s",
		QualitySelector:  "format-does-not-exist-999999",
	})
	if !errors.Is(err, ErrQualityUnavailable) && !errors.Is(err, ErrBackendFailed) {
		t.Fatalf("Download() error = %v, want ErrQualityUnavailable or ErrBackendFailed", err)
	}
}

func requireIntegrationURL(t *testing.T) string {
	t.Helper()

	if os.Getenv("RUN_INTEGRATION") != "1" {
		t.Skip("set RUN_INTEGRATION=1 to enable downloader integration tests")
	}

	binary := strings.TrimSpace(os.Getenv("YT_DLP_BIN"))
	if binary == "" {
		binary = "yt-dlp"
	}
	if _, err := exec.LookPath(binary); err != nil {
		t.Skipf("yt-dlp binary %q not available: %v", binary, err)
	}

	url := strings.TrimSpace(os.Getenv("TEST_VIDEO_URL"))
	if url == "" {
		t.Skip("set TEST_VIDEO_URL to a stable fixture URL for integration tests")
	}

	return url
}

func integrationAdapter(t *testing.T) *Adapter {
	t.Helper()

	adapter := NewAdapter()
	if backend := strings.TrimSpace(os.Getenv("YT_DLP_BIN")); backend != "" {
		adapter.WithBackendBinary(backend)
	}
	if ffmpeg := strings.TrimSpace(os.Getenv("FFMPEG_BIN")); ffmpeg != "" {
		adapter.WithFFmpegBinary(ffmpeg)
	}
	return adapter
}

func runIntegrationDownload(t *testing.T, adapter *Adapter, request Request) (Result, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	result, err := adapter.Download(ctx, request)
	if err != nil {
		return Result{}, err
	}

	if !strings.HasPrefix(result.OutputPath, filepath.Clean(request.OutputDir)) {
		t.Fatalf("OutputPath = %q, want path rooted under %q", result.OutputPath, request.OutputDir)
	}

	return result, nil
}
