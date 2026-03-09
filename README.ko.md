# TasQ

[English README](./README.md)

TasQ는 사람과 AI가 함께 작업할 때, 계획과 실행을 분리해서 안전하게 운영하도록 만든 셀프 호스팅 도구입니다.

## 이런 사용자에게 적합합니다
- Codex CLI, Gemini CLI 같은 에이전트를 실제 개발에 쓰는 경우
- 병렬 실행 상태를 태스크 단위로 눈에 보이게 관리하고 싶은 경우
- 작업이 꼬였을 때(실패, 만료, 고아 태스크) 복구 수단이 필요한 경우

## 핵심 개념
TasQ는 아래를 분리합니다.
- 구조 트리(Tree): 부모/자식 기반 계획 구조
- 실행 제약(Dependency): 선행 작업 관계
- 런타임 수명주기: claim, heartbeat, complete/fail/release

이 구조 덕분에 계획 수정과 병렬 실행을 동시에 안정적으로 다룰 수 있습니다.

## 쉬운 사용 시나리오
개발에 익숙하지 않은 사용자 기준으로는 아래 흐름을 권장합니다.

1. AI와 채팅으로 계획 초안 만들기
요구사항, 우선순위, 완료 기준을 정리합니다.

2. TasQ에 프로젝트/태스크 트리 생성
AI 또는 사용자가 태스크를 등록하고 의존성을 설정합니다.

3. 에이전트 병렬 실행
실행 가능한 태스크를 각 에이전트가 가져가서 동시에 처리합니다.

4. 사용자 개입은 게이트 지점에서만
실패/분기/우선순위 변경 시점에만 의사결정합니다.

5. 최종 머지/릴리즈 전 점검
회귀 스크립트와 체크리스트로 마지막 검증을 수행합니다.

## 사용자가 개입해야 하는 시점
- 실행 시작 전:
  - 태스크 크기(너무 큼/너무 작음) 승인
  - 의존성 및 capability 설정 확인
- 실행 중:
  - 좌측 Projects 배지로 이상 징후 확인
  - `Task Detail > Execution > Runtime Alerts`에서 상세 확인
  - 실패/정체 시 재시도, 분기, 재배치 결정
- 최종 머지 전:
  - 최종 회귀 스위트 실행
  - 릴리즈 체크리스트 확인

## 빠른 시작
```bash
docker compose up --build
```

- Web: http://localhost:5173
- API: http://localhost:8080
- DB: localhost:5432
- MCP (HTTP): http://localhost:8091/mcp

## Codex MCP 설정
현재 Codex CLI 환경에서는 stdio 대신 HTTP MCP transport 사용을 권장합니다.

```bash
docker compose up -d --build mcp
codex mcp add tasq-http --url http://localhost:8091/mcp
```

## AI/운영 참고 문서
- Agent API 계약: `tasq-backend/docs/agent_contract.md`
- 운영 가이드: `tasq-backend/docs/operations.md`
- 릴리즈 체크리스트: `tasq-backend/docs/release_checklist.md`
- MCP 세션 프로파일: `tasq-mcp/docs/session_profiles.md`

## 런타임 모니터링
- 좌측 프로젝트 배지:
  - 값 = `stale claims + heartbeat overdue + orphan in-progress + exhausted tasks`
- 우측 Task Detail:
  - `Execution > Runtime Alerts`에서 상세 목록 확인
  - heartbeat 기준값 조정 후 수동 refresh 가능
  - 큐 요약(`done/total`, `claimable`, `blocked`, `exhausted`) 제공

## 실시간 반영
TasQ는 아래 두 방식을 함께 사용합니다.
- SSE: 이벤트 기반 UI 갱신
- polling: stale lease, heartbeat overdue 같은 시간 기반 런타임 경보 보정

SSE 엔드포인트:
- `GET /projects/:project_id/stream`

SSE 이벤트 타입:
- `task-event`
  - payload는 task event row
  - 현재 다음 이벤트에 사용:
    - `task.created`
    - `task.content.updated`
    - `task.execution_policy.updated`
    - `task.capabilities.updated`
    - `task.status.updated`
    - `task.git.linked`
    - `task.reordered`
    - `task.moved`
    - `task.claimed`
    - `task.claim.heartbeat`
    - `task.claim.released`
    - `task.completed`
    - `task.failed`
    - `task.reconciled.stale_claim`
- `project-signal`
  - `task_events`에 남기기 애매한 케이스를 위한 경량 브로커 신호
  - 현재 다음 이벤트에 사용:
    - `task.deleted`
    - `project.deleted`

프론트 갱신 규칙:
- tree refresh:
  - `task.created`, `task.moved`, `task.reordered`, `task.status.updated`, `task.completed`, `task.failed`, `task.claimed`, `task.claim.released`, `task.reconciled.stale_claim`, `task.deleted`
- 선택된 task detail / observability refresh:
  - `task.content.updated`, `task.execution_policy.updated`, `task.capabilities.updated`, `task.git.linked`, `task.status.updated`, `task.completed`, `task.failed`, `task.claimed`, `task.claim.heartbeat`, `task.claim.released`, `task.reconciled.stale_claim`
- runtime alerts refresh:
  - claim / heartbeat / complete / fail / reconcile / delete 계열 이벤트

설계 메모:
- 삭제는 `task_events` row가 FK cascade로 함께 사라질 수 있기 때문에, post-delete UI 갱신 신호로는 `project-signal`을 사용합니다

## 워커 스포너
플래너 세션은 worker spawn spec JSON 파일을 만들고, 이후 아래 스크립트를 실행하면 됩니다.

```bash
cp ./examples/workers.sample.json /abs/path/to/workers.json
./scripts/spawn_workers.sh --spec-file /abs/path/to/workers.json
```

스포너 동작:
- worker spec마다 `codex exec` 워커 세션 1개씩 실행
- `GET /projects/:id/runtime-alerts`로 큐 진행 상황 감독
- 아직 진행 가능한 일이 있을 때만 워커 재기동
- 어떤 태스크든 `max_attempts`를 소진하면 즉시 중단

`max_attempts` 소진 시 즉시 멈추는 이유:
- TasQ는 `at-least-once` 실행 모델이라 워커 재시작 자체는 허용됨
- 끊긴 워커가 다시 떠서 unfinished task를 재-claim하는 것은 합리적임
- 하지만 `max_attempts`를 넘긴 태스크는 큐가 의도적으로 더 이상 배정하지 않음
- 이 상태에서 스포너가 워커만 계속 재기동하면 큐만 헛돌고 문제는 풀리지 않음
- 따라서 이 경우는 인간이 개입해 태스크를 수정하거나, `max_attempts`를 조정하거나, 근본 원인을 해결해야 함

## 바로 써볼 수 있는 프롬프트
### 1) 플래너 프롬프트
`tasq-http` MCP를 등록한 뒤 새 Codex CLI 세션에서 아래를 그대로 붙여 넣으면 됩니다.

- 템플릿 파일: [templates/planner_prompt_tasq.txt](/Users/jung-yeon-woo/Development/Projects/tasq/templates/planner_prompt_tasq.txt)
- 분해 가이드: [docs/planner_decomposition_guide.md](/Users/jung-yeon-woo/Development/Projects/tasq/docs/planner_decomposition_guide.md)
- 워커 스펙 샘플: [examples/workers.sample.json](/Users/jung-yeon-woo/Development/Projects/tasq/examples/workers.sample.json)
- 워커 스펙 스키마: [schemas/workers.schema.json](/Users/jung-yeon-woo/Development/Projects/tasq/schemas/workers.schema.json)

```text
You are a planning agent working with TasQ via MCP tools.

Create a project and actionable task tree for:
"Go-based YouTube Downloader CLI using Cobra, supporting video/audio download and quality options."

Rules:
- Use TasQ MCP tools to create one project, root tasks, child tasks, and dependency edges.
- Keep tasks small enough for one focused worker run.
- Use required_capabilities sparingly.
- Keep at least one root task immediately claimable by a generic worker.
- 같은 파일이나 모듈을 건드릴 가능성이 높다면 넓은 sibling 구조보다 수직 체인 구조를 우선해.
- 루트 태스크 수는 과도하게 늘리지 말고, 병렬화는 실제로 merge-safe한 경우에만 허용해.
- Prefer generic capability labels: go, cli, integration, testing, docs.
- Do not implement code in this session.
- At the end, report the numeric project_id.
- Also create a workers.json file in the current workspace with this shape:
  {
    "project_id": <numeric id>,
    "workspace": "<absolute workspace path>",
    "api_base": "http://127.0.0.1:8080",
    "lease_seconds": 300,
    "heartbeat_seconds": 60,
    "poll_seconds": 15,
    "workers": [
      {"agent_id": "worker-1", "capabilities": ["go","cli","docs"]},
      {"agent_id": "worker-2", "capabilities": ["go","integration","testing","docs"]}
    ]
  }

At the end, print:
- project_id
- concise tree summary
- dependency summary
- path to workers.json
```

### 2) 워커 자동 실행
```bash
cp ./examples/workers.sample.json /abs/path/to/workers.json
# 먼저 workers.json의 project_id와 workspace를 수정
./scripts/spawn_workers.sh --spec-file /abs/path/to/workers.json
```

### 3) 수동 워커 프롬프트
스포너 대신 워커 하나를 직접 띄우고 싶다면 아래 프롬프트를 사용하면 됩니다.

```text
You are a TasQ worker agent executing tasks from queue through MCP.

Use:
- project_id=<numeric project id>
- agent_id=worker-1
- capabilities=["go","cli","integration","testing","docs"]
- lease_seconds=300
- heartbeat_seconds=60

Rules:
- project_id must remain numeric.
- Claim only through TasQ MCP tools.
- Immediately fetch task context after claim.
- If work may take longer than 60 seconds, send tasq_heartbeat before lease expiry.
- 의미 있는 중간 단계가 끝났거나 위험한 수정/테스트 직전에는 checkpoint note도 남겨.
- task context에 `interrupted_runs`가 있으면, 편집 전에 가장 최근 interruption reason과 resume hint를 먼저 확인해.
- task context에 `effective_git_policy="required"`가 있으면, 편집 전에 `baseline` git ref를 먼저 링크하고 현재 attempt에서 새 `produced` ref를 남기기 전에는 complete 하지 마.
- If claim returns no task, stop cleanly.
- Finish with tasq_complete_task or tasq_fail_task.
- Include result_payload_version="v2" with summary, changes, paths, commands, tests, artifacts, next_risks.
- `result_md`는 handoff 품질이어야 하며 아래 섹션을 포함해야 해:
  - `## Summary`
  - `## Changes`
  - `## Verification`
  - `## Risks`

rerun 시에는 branch-first 전략을 권장해:
- rerun 경로용 새 브랜치를 만든다
- 그 브랜치에서 구현을 이어간다
- branch / base commit / produced commit을 task에 다시 링크한다
- 링크할 때 ref kind를 구분해:
  - `baseline`
  - `rerun_branch`
  - `produced`
```

## 에이전트 데모 실행
```bash
PROJECT_ID=1 ./scripts/agent_worker_demo.sh
PROJECT_ID=1 ACTION=fail ./scripts/agent_worker_demo.sh
PROJECT_ID=1 ACTION=release ./scripts/agent_worker_demo.sh
```

## 스모크/회귀 테스트
```bash
./scripts/claim_concurrency_smoke.sh
./scripts/dependency_cycle_race_smoke.sh
./scripts/topology_move_reorder_smoke.sh
./scripts/lease_reclaim_smoke.sh
./scripts/capability_claim_smoke.sh
./scripts/mcp_runtime_smoke.sh
./scripts/final_regression_suite.sh
```

## 기술 스택
- Backend: Go (`net/http`) + PostgreSQL
- Frontend: React + Vite
- Runtime: Docker Compose

## 인증/권한 (백엔드)
- `AUTH_MODE`: `off`(기본), `optional`, `required`
- `AUTH_TOKENS`: actor/scopes/project 범위를 담은 JSON 배열

## 다음 구현 후보
- Dependency 그래프 시각화
- 대시보드 수준의 런타임 알림 요약
