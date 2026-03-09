# Agent Git Execution Demo

This demo uses TasQ as a reviewed planning layer plus agent execution layer for a small software project.

Recommended project settings:
- `git_policy="required"`
- `execution_mode="agent_assisted"`
- `plan_state="draft"` during planning

Manual review step:
- open the generated project in the web UI
- inspect the task tree and dependency shape
- confirm git policy and execution mode
- change `plan_state` to `approved`
- only then start workers

Planner prompt:

```text
You are a planning agent working with TasQ via MCP tools.

Create a project and actionable task tree for:
"Go-based YouTube Downloader CLI using Cobra, supporting video/audio download and quality options."

Rules:
- Create the project with:
  - git_policy="required"
  - execution_mode="agent_assisted"
  - plan_state="draft"
- Use TasQ MCP tools to create the project, root tasks, child tasks, and dependency edges.
- Keep tasks small enough for one focused worker run.
- Keep at least one root task immediately claimable after approval.
- Prefer vertical decomposition when tasks touch the same files or modules.
- Use generic capabilities unless specialized routing is clearly needed.
- Create a workers.json file in the current workspace that conforms to schemas/workers.schema.json.
- Do not implement code in this session.

At the end, print:
- project_id
- concise tree summary
- dependency summary
- path to workers.json
```

Worker execution:

```bash
./scripts/spawn_workers.sh --spec-file /abs/path/to/workers.json
```

Use `examples/workers.sample.json` only if the planner failed to generate `workers.json`.

Expected behavior:
- spawner refuses to start while `plan_state` is still `draft`
- spawner refuses to start if `execution_mode` stays `manual`
- git-required tasks must link `baseline` before editing
- completed code work should link `produced`
- reruns should create and link `rerun_branch`
