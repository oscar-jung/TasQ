# Downloader Backend Contract

## Scope

This document defines the concrete contract between the Go application and the initial downloader backend. It narrows the broader integration strategy into an implementation-facing backend interface and the assumptions required for repeatable local and CI-like runs.

## Backend Choice

Use `yt-dlp` as the first backend and invoke it as an external process.

The application owns request validation, argument construction, preflight checks, and result normalization. The backend owns site extraction, media selection, and download execution.

## Required Local Binaries

Required in every environment that runs the real backend:

- `yt-dlp`

Conditionally required:

- `ffmpeg` when audio extraction, transcoding, or container remux is requested

The wrapper should resolve binaries in this order:

1. Explicit environment override such as `YT_DLP_BIN` or `FFMPEG_BIN`
2. Executable lookup on `PATH`

If the binary cannot be found, the wrapper should fail fast before attempting any download.

## Invocation Contract

The Go-side request contract should contain these fields:

| Field | Type | Notes |
| --- | --- | --- |
| `URL` | string | Required media source URL |
| `Mode` | enum | `video` or `audio` |
| `OutputDir` | string | Required writable directory |
| `FilenameTemplate` | string | Optional; default should be deterministic |
| `QualitySelector` | string | Optional; default is `best` |
| `ExtractAudio` | bool | Only valid for audio mode |
| `AudioFormat` | string | Optional output format when extraction is enabled |

The wrapper should construct `yt-dlp` arguments without shell interpolation. Pass arguments directly to `exec.Command` or an equivalent runner.

## Base Command Shape

Every real invocation should include a deterministic base set of flags:

```text
yt-dlp
  --no-progress
  --newline
  --print after_move:filepath
  --print format_id
  --output <template>
```

Mode-specific additions:

- `video`: default to `-f best`
- `audio` without extraction: default to `-f bestaudio`
- `audio` with extraction: add `-x` and `--audio-format <format>` and require `ffmpeg`
- explicit quality selector: replace the default `-f` value with the caller-provided selector

## Output Handling

Treat backend output as structured-but-minimal:

- parse `stdout` lines for the final file path and selected `format_id`
- preserve `stderr` for diagnostics
- capture the process exit code

Return a normalized result object with:

- requested mode
- resolved output path
- selected format id
- backend executable path
- raw exit code

The wrapper should verify that the reported output path exists before returning success.

## Error Handling

Normalize backend failures into application-level categories:

- `ErrBackendMissing`
- `ErrBackendFailed`
- `ErrOutputMissing`
- `ErrQualityUnavailable`
- `ErrFFmpegMissing`

Each normalized error should retain:

- a short category
- the backend exit code when available
- a trimmed `stderr` excerpt
- enough request metadata to reproduce the failure

## Repeatability Assumptions

For developer machines and CI-like runs, assume:

- the process can create and remove files in a temp workspace
- tests do not write into shared user directories
- the backend version is logged before execution
- fixture URLs are explicit and overrideable
- integration tests are opt-in, not part of the default unit-test path

Recommended environment variables:

- `RUN_INTEGRATION=1` to enable real backend execution
- `YT_DLP_BIN` to override the downloader binary location
- `FFMPEG_BIN` to override the ffmpeg location
- `TEST_VIDEO_URL` to override the fixture media URL

## CI-like Execution Expectations

CI-like jobs should:

- install `yt-dlp` before test execution
- install `ffmpeg` only for jobs that cover extraction or remux behavior
- run backend preflight checks before integration tests
- skip integration coverage when the required binaries or fixture URL are intentionally absent

To reduce flakiness, keep the first CI-like coverage narrow:

- one video download path
- one audio download path
- one explicit quality-selection path

## Non-Goals For The First Iteration

Do not add these in the initial backend wrapper:

- concurrent multi-download orchestration
- custom format-ranking logic in Go
- retry policies above the backend process
- implicit installation of external binaries
