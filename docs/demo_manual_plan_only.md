# Manual Plan-Only Demo

This demo uses TasQ only as a planning and tracking tool. No worker execution is intended.

Recommended project settings:
- `git_policy="optional"`
- `execution_mode="manual"`
- `plan_state="approved"`

Example scenario:
- a six-week Go backend study plan

Planner prompt:

```text
You are a planning agent working with TasQ via MCP tools.

Create a project and actionable task tree for:
"A six-week Go backend study plan for someone learning HTTP servers, SQL, concurrency, testing, and deployment basics."

Rules:
- Create the project with:
  - git_policy="optional"
  - execution_mode="manual"
  - plan_state="approved"
- This is a human-managed project, not an agent execution project.
- Prefer weekly milestones with child tasks for reading, practice, and review.
- Keep tasks specific and practical.
- Use dependencies only when one study unit clearly depends on another.
- Do not create workers.json.
- Do not implement anything.

At the end, print:
- project_id
- concise tree summary
- weekly milestone summary
```

Expected behavior:
- worker claims are blocked because the project stays in `manual`
- the user manages task status and markdown directly in the web UI
