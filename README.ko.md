# TasQ

[English README](./README.md)

TasQ는 사람과 AI가 함께 작업할 때, 계획과 실행을 분리해서 안전하게 운영하도록 만든 셀프 호스팅 도구입니다.

## 현재 상태
- 지금 안정적으로 쓸 수 있는 경로: 사람 중심 계획 수립, 사람이 개입하는 태스크 관리, 트리 수정, 런타임 관찰, rerun/invalidate.
- 아직 WIP로 봐야 하는 경로: MCP 기반 에이전트 실행, worker spawning, Git 보조 복구 흐름.
- 실전 기준 권장: 에이전트 경로는 작고 경계가 분명한 태스크에 우선 적용하고, 기본 사용 경로는 수동 플랜/사람 중심 운영으로 두는 편이 안정적입니다.

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

## Codex MCP 설정 (WIP)
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

## MCP / 에이전트 경로가 아직 WIP인 이유
TasQ는 이미 프로젝트 계획, claim 가능한 태스크 노출, lease/heartbeat 추적, MCP를 통한 Codex 스타일 worker 실행까지는 지원합니다. 아직 불안정한 부분은 프로젝트/태스크 모델이 아니라, 그 주변의 실행 plane입니다.

현재 WIP로 보는 이유:
- worker 프로세스가 아직 전용 runner service가 아니라 CLI 중심 스포너에 의존합니다
- 긴 작업은 여전히 lease/heartbeat/checkpoint 복구에 기대고 있고, durable execution layer가 충분히 강하지 않습니다
- rerun/invalidate는 TasQ 상태를 되돌리지만, 워크스페이스 복구는 아직 자동 action plane이 아니라 수동 Git 절차 안내에 가깝습니다
- MCP 및 에이전트 런타임은 TasQ 바깥의 클라이언트/runtime 특성에도 영향을 받습니다
- 웹 UI는 상태를 관찰하고 제어할 수 있지만, 아직 worker 프로세스를 직접 호스팅하거나 지속적으로 관리하지는 못합니다

실무적으로는 이렇게 보는 게 맞습니다:
- 사람 중심 계획 프로젝트는 바로 써도 됩니다
- AI가 계획을 짜고 사람이 승인하는 흐름도 충분히 쓸 만합니다
- 에이전트 실행은 작은 코드 태스크에서는 이미 유효합니다
- 하지만 큰 autonomous run은 아직 “안정적 자동화”보다는 “감독 가능한 실험”에 가깝습니다

기술적으로 앞으로 보강할 방향:
- 전용 runner/orchestrator service를 추가해 durable worker lifecycle을 관리
- reconnect, long-running command, partial progress handoff를 더 안정화
- Git recovery guidance를 명시적 확인 절차가 있는 action flow로 확장
- 웹 control plane을 강화해서 approval, dispatch, rerun, recovery를 더 직접적으로 수행
- agent를 끄더라도 manual-only 프로젝트가 1급 경로로 유지되도록 설계 지속

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

## 워커 스포너 (WIP)
플래너 세션은 worker spawn spec JSON 파일을 만들고, 이후 아래 스크립트를 실행하면 됩니다.

```bash
./scripts/spawn_workers.sh --spec-file /abs/path/to/workers.json
```

만약 planner가 `workers.json`을 만들지 못했다면, 아래 샘플을 수동 부트스트랩 용도로 사용할 수 있습니다.
- `examples/workers.sample.json`

스포너 동작:
- worker spec마다 `codex exec` 워커 세션 1개씩 실행
- `GET /projects/:id/runtime-alerts`로 큐 진행 상황 감독
- `claimable`과 별도로 `claimed`를 표시해서, 이미 다른 active worker가 잡고 있는 planned task를 큐 버그로 오해하지 않도록 함
- 아직 진행 가능한 일이 있을 때만 워커 재기동
- 어떤 태스크든 `max_attempts`를 소진하면 즉시 중단
- `plan_state`가 `approved`가 아니면 시작하지 않음
- `execution_mode`가 `manual`이면 시작하지 않음
- 정상 종료, `Ctrl+C`, `TERM` 시에는 자신이 띄운 worker child process도 함께 정리함
- `kill -9` 같은 강제 종료에서는 child worker가 남을 수 있어 수동 정리가 필요할 수 있음
- 즉, 현재 스포너는 유용한 실행 경로이지만 최종 형태의 production-grade runner는 아닙니다

## 플랜 승인과 실행 모드
TasQ는 project 레벨에서 계획 검토와 agent 실행을 분리한다.

- `execution_mode`
  - `manual`: 계획과 추적만 수행, worker claim 차단
  - `agent_assisted`: 사람이 승인한 뒤 worker 실행 가능
  - `agent_autonomous`: 승인 후 최소 개입으로 worker 실행 가능
- `plan_state`
  - `draft`: 아직 검토 중, worker claim 차단
  - `approved`: 실행 모드가 허용하면 worker claim 가능
  - `archived`: 기록 보관 상태

권장 기본값:
- 사람만 쓰는 플랜 project:
  - `git_policy="optional"`
  - `execution_mode="manual"`
  - `plan_state="approved"`
- agent 중심 코드 project:
  - `git_policy="required"`
  - `execution_mode="agent_assisted"`
  - `plan_state="draft"`

권장 흐름:
1. planner가 project와 tree를 생성
2. 사람이 웹 UI에서 검토
3. 사람이 `plan_state`를 `approved`로 변경
4. 그 다음에만 worker 또는 spawner 시작

`max_attempts` 소진 시 즉시 멈추는 이유:
- TasQ는 `at-least-once` 실행 모델이라 워커 재시작 자체는 허용됨
- 끊긴 워커가 다시 떠서 unfinished task를 재-claim하는 것은 합리적임
- 하지만 `max_attempts`를 넘긴 태스크는 큐가 의도적으로 더 이상 배정하지 않음
- 이 상태에서 스포너가 워커만 계속 재기동하면 큐만 헛돌고 문제는 풀리지 않음
- 따라서 이 경우는 인간이 개입해 태스크를 수정하거나, `max_attempts`를 조정하거나, 근본 원인을 해결해야 함

## 바로 써볼 수 있는 프롬프트
### 1) 플래너 프롬프트
`tasq-http` MCP를 등록한 뒤 새 Codex CLI 세션에서 아래를 그대로 붙여 넣으면 됩니다.

- 템플릿 파일: [templates/planner_prompt_tasq.txt](/path/to//tasq/templates/planner_prompt_tasq.txt)
- 분해 가이드: [docs/planner_decomposition_guide.md](/path/to//tasq/docs/planner_decomposition_guide.md)
- 리뷰 체크리스트: [docs/planner_review_checklist.md](/path/to//tasq/docs/planner_review_checklist.md)
- 에이전트 코드 프로젝트 예시: [docs/planner_example_agent_code_project.md](/path/to//tasq/docs/planner_example_agent_code_project.md)
- 워커 스펙 샘플: [examples/workers.sample.json](/path/to//tasq/examples/workers.sample.json)
- 워커 스펙 스키마: [schemas/workers.schema.json](/path/to//tasq/schemas/workers.schema.json)
- 에이전트 실행 데모: [docs/demo_agent_git_execution.md](/path/to//tasq/docs/demo_agent_git_execution.md)
- 수동 플랜 데모: [docs/demo_manual_plan_only.md](/path/to//tasq/docs/demo_manual_plan_only.md)

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
- agent 중심 코드 프로젝트라면 project를 다음과 같이 생성해.
  - `git_policy="required"`
  - `execution_mode="agent_assisted"`
  - `plan_state="draft"`
- 사람만 쓰는 플랜 프로젝트라면 보통 다음이 맞다.
  - `git_policy="optional"`
  - `execution_mode="manual"`
  - `plan_state="approved"`
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
- checklist review summary
```

worker 시작 전에는:
- 웹 UI에서 project를 검토하고
- `plan_state`를 `approved`로 바꾸고
- `execution_mode`가 agent 모드인지 확인해

### 2) 워커 자동 실행 (WIP)
기본 경로:
```bash
./scripts/spawn_workers.sh --spec-file /abs/path/to/workers.json
```

planner가 `workers.json`을 만들지 못한 경우의 대체 경로:
```bash
cp ./examples/workers.sample.json /abs/path/to/workers.json
# 먼저 workers.json의 project_id와 workspace를 수정
./scripts/spawn_workers.sh --spec-file /abs/path/to/workers.json
```

### 3) 수동 워커 프롬프트 (WIP)
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
- 복구 시작점으로는 `latest_interruption`을 먼저 보고, 필요하면 `interrupted_runs` 전체 히스토리를 추가로 확인해.
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
- rerun 메타데이터가 충분한지 판단할 때는 임의 추정보다 Task Detail의 서버 제공 `git_recovery` 요약을 우선 기준으로 봐.
```

## 에이전트 데모 실행 (WIP)
```bash
PROJECT_ID=1 ./scripts/agent_worker_demo.sh
PROJECT_ID=1 ACTION=fail ./scripts/agent_worker_demo.sh
PROJECT_ID=1 ACTION=release ./scripts/agent_worker_demo.sh
```

## 수동 플랜 데모 실행 방법
planner가 구조만 만들고, 이후에는 사람이 웹 UI에서 직접 관리하고 싶을 때의 흐름이다.

1. 서비스를 띄운다.
```bash
docker compose up -d
curl -sS http://localhost:8080/healthz
```
2. Codex에 TasQ MCP가 등록되어 있는지 확인한다.
```bash
codex mcp list
```
`tasq-http`가 없다면:
```bash
codex mcp add tasq-http --url http://localhost:8091/mcp
```
3. 이 저장소에서 새 Codex 세션을 연다.
```bash
cd /path/to//tasq
codex
```
4. [docs/demo_manual_plan_only.md](/path/to//tasq/docs/demo_manual_plan_only.md)에 있는 planner 프롬프트를 그대로 붙여 넣는다.
5. planner가 project를 만들고 numeric `project_id`를 출력할 때까지 기다린다.
6. 웹 UI [http://localhost:5173](http://localhost:5173) 에서 생성된 project를 확인한다.
7. 이후에는 웹 UI에서 직접 관리한다.
   - task spec/result 수정
   - task status 변경
   - 필요 시 rerun/invalidate 사용

기대 동작:
- project는 `execution_mode="manual"` 상태를 유지한다
- project는 `plan_state="approved"` 상태를 유지한다
- worker claim과 spawner 실행은 막힌다

## 수동 플랜 데모
worker 없이 planner가 만든 구조만 쓰고 싶다면 [docs/demo_manual_plan_only.md](/path/to//tasq/docs/demo_manual_plan_only.md)를 참고해.

## 에이전트 + Git 데모 실행 방법 (WIP)
planner가 project와 tree를 만들고, 승인 후 worker가 실제 코드 작업까지 수행하는 흐름이다.

1. 서비스를 띄운다.
```bash
docker compose up -d
curl -sS http://localhost:8080/healthz
```
2. Codex에 TasQ MCP가 등록되어 있는지 확인한다.
```bash
codex mcp list
```
`tasq-http`가 없다면:
```bash
codex mcp add tasq-http --url http://localhost:8091/mcp
```
3. 대상 workspace에서 새 planner 세션을 연다.
```bash
cd /absolute/path/to/your/workspace
codex
```
4. [docs/demo_agent_git_execution.md](/path/to//tasq/docs/demo_agent_git_execution.md)의 planner 프롬프트를 그대로 붙여 넣는다.
5. planner가 다음을 끝낼 때까지 기다린다.
   - project 생성
   - numeric `project_id` 출력
   - `workers.json` 생성
6. 웹 UI [http://localhost:5173](http://localhost:5173) 에서 project를 검토하고 승인한다.
   - `git_policy="required"` 유지
   - `execution_mode`는 `agent_assisted` 또는 `agent_autonomous`
   - `plan_state`를 `draft`에서 `approved`로 변경
7. worker를 시작한다.
```bash
cd /path/to//tasq
./scripts/spawn_workers.sh --spec-file /absolute/path/to/your/workspace/workers.json
```
8. 웹 UI에서 실행 상태를 관찰한다.
   - task status
   - interrupted runs
   - checkpoints
   - Git recovery summary
9. upstream task를 다시 해야 하면 Task Detail에서 rerun/invalidate를 사용하고, Git은 branch-first로 다룬다.

기대 동작:
- 승인 전에는 spawner가 시작되지 않는다
- Git required task는 편집 전에 `baseline`을 링크해야 한다
- 성공한 코드 task는 `produced`를 남겨야 한다
- rerun 흐름에서는 `rerun_branch`를 링크하는 것이 맞다

현재는 다음 기대치를 두는 것이 맞습니다:
- 작은 구현 태스크일수록 잘 맞습니다
- 런타임 복구 시 사람의 수동 개입이 중간중간 필요할 수 있습니다
- Git recovery guidance는 제공되지만, 실제 워크스페이스 되돌리기는 아직 수동 절차입니다
- 지금 당장 가장 안정적인 사용법은 “TasQ로 계획/추적/검토를 하고, 실행은 제한적으로 에이전트를 붙이는 방식”입니다

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
- durable worker 실행을 위한 전용 action plane / runner service
- 웹 control plane에서 더 안전하게 수행하는 Git-aware recovery actions
- 장시간 agent task에 대한 런타임 안정성 강화
- 대시보드 수준의 런타임 알림 요약
