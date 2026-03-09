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
  - 값 = `stale claims + heartbeat overdue + orphan in-progress`
- 우측 Task Detail:
  - `Execution > Runtime Alerts`에서 상세 목록 확인
  - heartbeat 기준값 조정 후 수동 refresh 가능

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
