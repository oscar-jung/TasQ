# Downloader Integration Runbook

## Purpose

This runbook explains how to execute the downloader integration checks locally, what environment assumptions they require, and how to diagnose common backend failures.

## What The Integration Checks Cover

The opt-in integration suite exercises the real downloader adapter against `yt-dlp` for:

- video download
- audio download
- explicit quality-selection execution
- a failure path for an unavailable quality selector

These checks validate the adapter's process invocation and error mapping against the real backend rather than the fake runner used in unit tests.

## Prerequisites

Required:

- Go 1.26 or newer in this demo workspace
- `yt-dlp` installed and available on `PATH`, or exposed through `YT_DLP_BIN`
- a stable media fixture URL provided through `TEST_VIDEO_URL`

Optional:

- `ffmpeg` installed and available on `PATH`, or exposed through `FFMPEG_BIN`, if you extend coverage to extraction or remux flows

## Environment Variables

- `RUN_INTEGRATION=1` enables the real-backend tests
- `TEST_VIDEO_URL` points to the fixture media URL used by the tests
- `YT_DLP_BIN` overrides the downloader binary path
- `FFMPEG_BIN` overrides the ffmpeg binary path
- `TEST_QUALITY_SELECTOR` optionally overrides the quality selector used by the quality-selection success case

## Running The Checks

Run the full package test suite:

```bash
RUN_INTEGRATION=1 \
TEST_VIDEO_URL='https://example.test/watch?v=fixture' \
go test ./...
```

Run only the downloader package:

```bash
RUN_INTEGRATION=1 \
TEST_VIDEO_URL='https://example.test/watch?v=fixture' \
go test ./downloader -run Integration
```

If `yt-dlp` is not on `PATH`, add `YT_DLP_BIN`:

```bash
RUN_INTEGRATION=1 \
TEST_VIDEO_URL='https://example.test/watch?v=fixture' \
YT_DLP_BIN="$HOME/bin/yt-dlp" \
go test ./downloader -run Integration
```

## Fixture Guidance

Use a fixture URL that is:

- publicly accessible from the environment running the tests
- stable enough that it is unlikely to disappear between runs
- short enough to keep downloads fast
- legally and operationally acceptable for repeatable automated checks

If no fixture URL is provided, the integration tests skip rather than fail.

## Troubleshooting

`yt-dlp` not found:

- confirm `yt-dlp --version` works locally
- set `YT_DLP_BIN` if the binary is installed outside `PATH`

Download exits non-zero:

- rerun the command manually with the same URL and selector
- inspect network connectivity, upstream throttling, or extractor changes
- update `yt-dlp` if the target site behavior has changed

Quality-selection test fails:

- verify the chosen `TEST_QUALITY_SELECTOR` is valid for the fixture URL
- omit `TEST_QUALITY_SELECTOR` to fall back to `best`
- expect the failure-case test to use an intentionally invalid selector

Audio or remux flows fail:

- confirm `ffmpeg -version` works locally
- set `FFMPEG_BIN` if ffmpeg is installed outside `PATH`
- avoid extraction-specific checks in environments that do not install ffmpeg

Tests skip unexpectedly:

- confirm `RUN_INTEGRATION=1` is set
- confirm `TEST_VIDEO_URL` is non-empty
- confirm the selected binaries are reachable from the test environment
