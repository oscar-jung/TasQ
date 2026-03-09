package downloader

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type Mode string

const (
	ModeVideo Mode = "video"
	ModeAudio Mode = "audio"
)

var (
	ErrBackendMissing     = errors.New("downloader backend missing")
	ErrBackendFailed      = errors.New("downloader backend failed")
	ErrOutputMissing      = errors.New("downloader output missing")
	ErrQualityUnavailable = errors.New("downloader quality unavailable")
	ErrFFmpegMissing      = errors.New("ffmpeg missing")
)

type Request struct {
	URL              string
	Mode             Mode
	OutputDir        string
	FilenameTemplate string
	QualitySelector  string
	ExtractAudio     bool
	AudioFormat      string
}

type Result struct {
	RequestedMode Mode
	OutputPath    string
	FormatID      string
	BackendPath   string
	ExitCode      int
}

type Error struct {
	Kind     error
	ExitCode int
	Stderr   string
	Request  Request
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}

	message := e.Kind.Error()
	if e.ExitCode != 0 {
		message = fmt.Sprintf("%s (exit_code=%d)", message, e.ExitCode)
	}
	if e.Stderr != "" {
		message = fmt.Sprintf("%s: %s", message, e.Stderr)
	}
	return message
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Kind
}

type Runner interface {
	Run(ctx context.Context, binary string, args []string) ([]byte, []byte, int, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, binary string, args []string) ([]byte, []byte, int, error) {
	cmd := exec.CommandContext(ctx, binary, args...)
	stdout, err := cmd.Output()
	if err == nil {
		return stdout, nil, 0, nil
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return stdout, exitErr.Stderr, exitErr.ExitCode(), nil
	}

	return stdout, nil, 0, err
}

type Adapter struct {
	backendBinary string
	ffmpegBinary  string
	lookPath      func(string) (string, error)
	runner        Runner
}

func NewAdapter() *Adapter {
	return &Adapter{
		lookPath: exec.LookPath,
		runner:   ExecRunner{},
	}
}

func (a *Adapter) WithRunner(runner Runner) *Adapter {
	if runner != nil {
		a.runner = runner
	}
	return a
}

func (a *Adapter) WithLookPath(lookPath func(string) (string, error)) *Adapter {
	if lookPath != nil {
		a.lookPath = lookPath
	}
	return a
}

func (a *Adapter) WithBackendBinary(binary string) *Adapter {
	a.backendBinary = strings.TrimSpace(binary)
	return a
}

func (a *Adapter) WithFFmpegBinary(binary string) *Adapter {
	a.ffmpegBinary = strings.TrimSpace(binary)
	return a
}

func (a *Adapter) Download(ctx context.Context, request Request) (Result, error) {
	if err := validateRequest(request); err != nil {
		return Result{}, err
	}

	backendPath, err := a.resolveBinary(a.backendBinary, "YT_DLP_BIN", "yt-dlp")
	if err != nil {
		return Result{}, &Error{Kind: ErrBackendMissing, Request: request}
	}

	if request.ExtractAudio {
		if _, err := a.resolveBinary(a.ffmpegBinary, "FFMPEG_BIN", "ffmpeg"); err != nil {
			return Result{}, &Error{Kind: ErrFFmpegMissing, Request: request}
		}
	}

	args := BuildArgs(request)
	stdout, stderr, exitCode, runErr := a.runner.Run(ctx, backendPath, args)
	if runErr != nil {
		return Result{}, fmt.Errorf("run downloader backend: %w", runErr)
	}

	if exitCode != 0 {
		kind := ErrBackendFailed
		if request.QualitySelector != "" && strings.Contains(strings.ToLower(string(stderr)), "format") {
			kind = ErrQualityUnavailable
		}
		return Result{}, &Error{
			Kind:     kind,
			ExitCode: exitCode,
			Stderr:   trimDiagnostic(stderr),
			Request:  request,
		}
	}

	outputPath, formatID, err := parseOutput(stdout)
	if err != nil {
		return Result{}, &Error{Kind: ErrOutputMissing, Request: request}
	}

	if _, err := os.Stat(outputPath); err != nil {
		return Result{}, &Error{Kind: ErrOutputMissing, Request: request}
	}

	return Result{
		RequestedMode: request.Mode,
		OutputPath:    outputPath,
		FormatID:      formatID,
		BackendPath:   backendPath,
		ExitCode:      exitCode,
	}, nil
}

func BuildArgs(request Request) []string {
	template := request.FilenameTemplate
	if strings.TrimSpace(template) == "" {
		template = "%(title)s.%(ext)s"
	}

	quality := strings.TrimSpace(request.QualitySelector)
	if quality == "" {
		quality = defaultQuality(request)
	}

	args := []string{
		"--no-progress",
		"--newline",
		"--print", "after_move:filepath",
		"--print", "format_id",
		"--output", filepath.Join(request.OutputDir, template),
		"-f", quality,
	}

	if request.Mode == ModeAudio && request.ExtractAudio {
		args = append(args, "-x")
		if format := strings.TrimSpace(request.AudioFormat); format != "" {
			args = append(args, "--audio-format", format)
		}
	}

	args = append(args, request.URL)
	return args
}

func validateRequest(request Request) error {
	if strings.TrimSpace(request.URL) == "" {
		return errors.New("url is required")
	}
	if strings.TrimSpace(request.OutputDir) == "" {
		return errors.New("output dir is required")
	}
	switch request.Mode {
	case ModeVideo, ModeAudio:
	default:
		return fmt.Errorf("unsupported mode %q", request.Mode)
	}
	if request.ExtractAudio && request.Mode != ModeAudio {
		return errors.New("audio extraction requires audio mode")
	}
	return nil
}

func defaultQuality(request Request) string {
	if request.Mode == ModeAudio {
		return "bestaudio"
	}
	return "best"
}

func parseOutput(stdout []byte) (string, string, error) {
	lines := strings.Split(string(stdout), "\n")
	nonEmpty := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			nonEmpty = append(nonEmpty, line)
		}
	}
	if len(nonEmpty) < 2 {
		return "", "", errors.New("missing output metadata")
	}
	return nonEmpty[len(nonEmpty)-2], nonEmpty[len(nonEmpty)-1], nil
}

func trimDiagnostic(stderr []byte) string {
	text := strings.TrimSpace(string(stderr))
	if len(text) <= 240 {
		return text
	}
	return text[:240]
}

func (a *Adapter) resolveBinary(explicit string, envVar string, fallback string) (string, error) {
	if path := strings.TrimSpace(explicit); path != "" {
		return path, nil
	}
	if path := strings.TrimSpace(os.Getenv(envVar)); path != "" {
		return path, nil
	}
	return a.lookPath(fallback)
}
