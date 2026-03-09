# TasQ Planner Example: Agent-driven Code Project

Goal:
- Build a Go YouTube Downloader CLI.

## Good decomposition
```text
Root: CLI contract
  └─ Define Cobra command structure and shared flags
     └─ Add shared option validation helpers

Root: Download engine
  └─ Define downloader interface and yt-dlp adapter
     └─ Wire audio download flow
        └─ Add audio command tests
     └─ Wire video download flow
        └─ Add video command tests

Root: Handoff and docs
  └─ Write user-facing README usage guide
```

Why this is good:
- roots represent distinct delivery streams
- most file-conflicting work is chained vertically
- tests sit below the implementation they validate
- rerun scope is easy to understand

## Risky decomposition
```text
Root: Project
  ├─ Add Cobra root
  ├─ Add shared flags
  ├─ Add validation
  ├─ Add downloader interface
  ├─ Add audio command
  ├─ Add video command
  ├─ Add tests
  └─ Update README
```

Why this is risky:
- too many siblings touch the same files
- workers will collide in the same modules
- rerun scope becomes unclear
- capability routing is likely to be over-specified

## Planner self-check
- Are the first runnable tasks obvious?
- If task `A` changes later, is the rerun scope obvious?
- Would two workers likely edit the same file at the same time?
- Could a new worker understand each task result without rereading the whole repo?
