# Downloader Integration Strategy

## Goal

Provide one stable integration seam for media downloads so downstream tasks can build a Go wrapper, add integration tests, and document operator prerequisites without revisiting the execution model.

## Recommended Backend

Use `yt-dlp` as the initial downloader backend and invoke it as an external process from Go.

This choice keeps the application code thin and avoids reimplementing site extraction logic. `yt-dlp` already supports separate video and audio workflows, format selection, and structured output that can be consumed from tests.

## Integration Boundary

Define the downloader integration around a single command runner abstraction:

- Input: URL, media mode (`video` or `audio`), output directory, filename template, and quality selector.
- Execution: spawn `yt-dlp` with explicit flags and capture `stdout`, `stderr`, exit code, and output file paths.
- Output: normalized result object with requested format, resolved file path, selected format id, and backend diagnostics.

The Go side should treat `yt-dlp` as replaceable infrastructure. Only one package should know how command-line arguments are assembled.

## Command Strategy

Use a deterministic base invocation for every integration test:

```text
yt-dlp --no-progress --newline --print after_move:filepath --print format_id
```

Add mode-specific flags on top of that base:

- Video download: prefer a combined stream when available, otherwise allow muxed best video plus best audio.
- Audio-only download: use `-x --audio-format mp3` only if ffmpeg is available; otherwise allow the original audio container and report the actual extension.
- Quality selection: accept either a symbolic policy such as `best` or an explicit format selector string passed through to `-f`.

The wrapper should always set an explicit output template rooted in a task-owned temp directory so tests can clean up predictably.

## Runtime Prerequisites

Required:

- Go toolchain version used by the repository.
- `yt-dlp` present on `PATH`.
- Network access to the target media host during integration runs.

Conditionally required:

- `ffmpeg` on `PATH` for audio extraction or remux flows that need transcoding.

Recommended preflight checks:

- Verify `yt-dlp --version` before running integration tests.
- Verify `ffmpeg -version` only for tests that request extraction or container conversion.
- Skip, rather than fail, integration tests when required external tools are missing.

## Test Strategy

Split coverage into two layers:

1. Unit tests for argument construction and result parsing with a fake command runner.
2. Integration tests that execute the real backend behind an explicit opt-in gate.

Suggested integration test gate:

- Require `RUN_INTEGRATION=1`.
- Allow `YT_DLP_BIN` and `FFMPEG_BIN` overrides for non-default installations.
- Allow `TEST_VIDEO_URL` override, but provide a stable default fixture URL in test code or config once chosen.

## Minimum Integration Matrix

Cover these scenarios first:

| Scenario | Backend expectation | Verification |
| --- | --- | --- |
| Video download, default quality | command exits successfully | output file exists and has a video container extension |
| Audio download, no transcode requirement | command exits successfully | output file exists and selected mode is audio |
| Audio extraction with ffmpeg available | extraction path is used | output file exists with requested extension |
| Explicit quality selector | selector is passed through | reported format id matches the requested selection or the command fails clearly |
| Missing prerequisite | test is skipped | skip reason names the missing binary |

## Quality Selection Rules

Keep the first implementation narrow:

- Support `best` as the default policy.
- Support raw format selector strings for advanced use cases.
- Return the resolved `format_id` from backend output so tests can assert selection behavior.

Do not add a custom ranking layer in Go during the initial stream. Let `yt-dlp` remain the source of truth for format resolution.

## Failure Handling

Normalize these failures in the wrapper contract:

- backend binary missing
- backend exited non-zero
- no output file reported
- requested quality unavailable
- ffmpeg required but missing

Include `stderr` in the normalized error for diagnosis, but trim excessive output before surfacing it in user-facing logs.

## Downstream Work Unblocked By This Document

- implement a Go downloader adapter around process execution
- add a backend preflight helper for integration tests
- add opt-in integration tests for video, audio, and quality selection
- document local setup for `yt-dlp` and optional `ffmpeg`
