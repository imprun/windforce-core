# ADR 0013: Canonical Invocation API와 Trigger admission 경계

## Status

Accepted (2026-07-26) — issue [#137](https://github.com/imprun/windforce-core/issues/137). 구현 순서와 제거 gate는 [Canonical Invocation과 Trigger 구현 계획](../canonical-invocation-trigger-implementation-plan.md)에 고정한다.

이 결정은 [ADR 0011](0011-public-api-client-tokens.md)의 별도 Public API plane과 공개 응답 식별자 결정을 갱신하고, [ADR 0012](0012-server-worker-standalone-roles.md)의 외부 adapter용 `/execution/v1` 결정과 네 개 HTTP plane 유지 결정을 갱신한다. Client token 수명주기, InputConfig, server/worker/standalone 배치와 `/worker/v1` 계약은 유지한다.

## Context

Windforce Core는 같은 admission 의미를 세 HTTP 표면으로 제공한다. Operator와 도구는 `/api/w/{workspace}/jobs/run/...`, 신뢰된 adapter와 Python SDK는 `/execution/v1/workspaces/{workspace}/runs`, client token 호출자는 `/api/v1/w/{workspace}/run/...`을 사용한다. 세 handler는 모두 `execution.Service.CreateRun`으로 수렴하지만 인증 주체, 요청 필드, Run/Job 식별자, wait 응답과 SDK가 다르므로 기능을 추가할 때마다 계약이 갈라진다.

실제 소비자 조사에서 Gale은 `/execution/v1`의 idempotent Run lifecycle을 직접 사용하고, Imprun Cloud gateway는 tenant 경로 아래로 `/execution/v1`을 전달하며, Core Python SDK와 구형 dhworker trigger도 Execution API 계약을 소비한다. 반면 새 `wf-triggers`는 `/api/v1/w/.../run/.../wait`를 사용한다. 따라서 한 repository만 먼저 기존 경로를 삭제할 수는 없지만 known consumer를 같은 release set으로 이전하면 두 계약을 영구적인 정본으로 유지할 이유도 없다.

Core의 모델에서 Run과 Job은 같은 이름의 중복 표현이 아니다. Run은 호출자가 추적하는 논리적 실행과 결과를 소유하고, Job은 Run을 실행하기 위한 queue, lease, priority와 attempt를 소유한다. 외부 API에서 Job ID를 실행 식별자로 사용하면 worker queue 구현을 호출자 계약에 노출한다.

Windmill은 HTTP와 protocol listener가 공통한 durable job admission 경계로 수렴하는 참고 사례다. Nuclio는 protocol별 trigger가 공통 Trigger/EventProcessor 경계로 직접 제출되는 참고 사례다. Windforce Core는 영속 Run, release pinning, idempotency, worker queue와 HITL을 가지므로 Windmill의 단일 admission 원칙을 중심으로 삼고 Nuclio의 Trigger SPI와 lifecycle을 보조적으로 적용한다.

## Decision

### 1. 세 개의 지속 가능한 plane만 둔다

최종 HTTP 경계는 Control `/api/w`, Invocation `/api/v1`, Worker `/worker/v1`이다. 현재의 `/execution/v1`과 control-plane run/webhook submission 경로는 v0.3.0 cutover에서 제거할 legacy surface이며 독립 plane이 아니다.

- Control plane은 source, release, app, client/service principal, trigger 설정과 operator용 Run/Job 관리를 소유한다.
- Invocation plane은 Run 생성, wait, 상태, 결과, 취소와 호출에 필요한 app contract 조회를 소유한다.
- Worker plane은 worker 등록, claim, lease heartbeat, log, result completion과 artifact 전송을 소유한다.

### 2. `/api/v1`을 유일한 외부 Invocation API로 한다

Canonical v1 경로는 다음과 같다.

```text
POST /api/v1/workspaces/{workspace}/runs
POST /api/v1/workspaces/{workspace}/runs/wait?timeout={duration}
GET  /api/v1/workspaces/{workspace}/runs/{run_id}
GET  /api/v1/workspaces/{workspace}/runs/{run_id}/result
POST /api/v1/workspaces/{workspace}/runs/{run_id}/cancel
GET  /api/v1/workspaces/{workspace}/apps/{app}
GET  /api/v1/openapi.json
```

Run 생성 body는 `app`, `action`, opaque `input`과 선택적인 `correlation_id`만 caller-controlled 필드로 받는다. Idempotency key의 canonical transport는 `Idempotency-Key` header다. `client_id`, `created_by`, `permissioned_as`는 인증 주체에서 파생하고 caller가 지정할 수 없다. 임의 환경변수를 실행에 넣는 `env`는 canonical API에서 허용하지 않으며 InputConfig, Variable, Resource 또는 Action input 계약을 사용한다.

비동기 생성은 새 Run에 `201`, idempotent replay에 `200`을 반환하고 `run_id`, `state`, `replayed`를 포함한다. `Location`과 `X-WF-Run-Id`는 canonical Run URL과 ID를 제공한다. Wait가 제한시간 안에 완료되면 raw Action result를 반환하고, 완료되지 않으면 `202`와 현재 Run 표현을 반환한다. Canonical Invocation API는 Job ID를 반환하지 않는다.

### 3. 외부 lifecycle은 Run, 내부 queue 단위는 Job으로 고정한다

Run은 caller-visible idempotency, 상태, result, cancel과 retention의 정본이다. Job은 Run을 수행하는 내부 queue record이며 worker claim, lease, priority와 attempt를 소유한다. 하나의 Run에 대한 실행 재시도는 외부 Run ID를 바꾸지 않는다.

Control plane은 운영 진단을 위해 Run과 연결된 Job ID와 attempt를 표시할 수 있지만 Invocation API와 SDK는 이를 안정 계약으로 노출하지 않는다. 기존 외부 `job_id`와 `X-WF-Job-Id`는 v0.3.0 cutover에서 제거한다.

### 4. 경로가 아니라 principal과 scope로 권한을 구분한다

Invocation API는 다음 principal을 같은 route에서 인증한다.

- Operator principal은 기존 instance admin 또는 workspace token에서 파생하며 해당 workspace의 모든 Run을 생성·조회·취소할 수 있다.
- Client principal은 기존 `wfk_` token에서 파생하며 허용된 app/action을 호출하고 자신이 생성한 Run만 조회·결과 확인·취소할 수 있다.
- Service principal은 control plane에서 등록·회전·revoke하는 engine-issued `wfs_` token에서 파생하며 workspace, scope와 선택적인 app/action allowlist를 가진다.

최소 scope는 `runs:create`, `runs:read:own`, `runs:read:any`, `runs:cancel:own`, `runs:cancel:any`, `apps:read`로 정의한다. Existing client token은 호환을 위해 `runs:create`, `runs:read:own`, `runs:cancel:own`, `apps:read`를 기본으로 갖는다. Service token 원문은 발급 시 한 번만 반환하고 state에는 hash만 저장한다.

Windforce Core는 자체 OIDC 사용자 디렉터리나 Imprun 전용 인증을 추가하지 않는다. Hosted product는 자신의 사용자 인증을 처리한 뒤 engine-issued operator/service credential로 Core와 통신하고, Core audit subject에는 검증된 principal만 기록한다.

### 5. AdmissionService를 유일한 실행 접수 정본으로 한다

모든 canonical HTTP handler, CLI와 built-in trigger는 하나의 in-process AdmissionService를 호출한다. AdmissionService는 principal authorization, active release resolution과 pinning, InputConfig merge, schema validation, idempotency, Run과 최초 Job의 영속 생성을 한 transaction 의미로 제공한다.

기존 `execution.Service.CreateRun`의 의미는 이 경계로 이동하며 점진적으로 명명과 interface를 정리한다. Adapter는 queue table, catalog file이나 worker API에 직접 쓰지 않는다.

### 6. Trigger를 first-class resource와 SPI로 만든다

Trigger definition은 workspace control plane의 `/api/w/{workspace}/triggers`에서 관리한다. 공통 필드는 ID, name, kind, enabled, target app/action, principal 또는 credential reference, delivery/idempotency policy와 protocol-specific config다. Secret 원문은 Trigger representation, audit와 log에 포함하지 않는다.

Built-in trigger는 공통 lifecycle과 submitter 경계를 구현한다.

```text
Trigger: Initialize -> Start -> Stop
TriggerEvent: trigger_id, delivery_id, correlation_id, target, input/raw payload, safe metadata
TriggerSubmitter: TriggerEvent -> AdmissionService -> Run admission
```

HTTP Invocation API는 범용 동기/비동기 호출 표면이다. Configured webhook, schedule, RabbitMQ와 이후 protocol adapter는 Trigger SPI를 구현한다. Server 안의 trigger는 자신의 HTTP API를 loopback 호출하지 않고 AdmissionService를 직접 호출한다. 다른 프로세스나 언어의 trigger는 service principal로 canonical Invocation API를 호출한다.

### 7. Protocol delivery는 at-least-once와 durable admission을 기준으로 한다

Broker message는 Run과 최초 Job이 durable하게 admission된 뒤 ACK한다. DB 또는 queue admission의 일시적 실패는 NACK/requeue하고, validation이나 존재하지 않는 target 같은 terminal rejection은 delivery failure로 기록한 뒤 adapter 정책에 따라 dead-letter 또는 ACK한다. Action 실행 실패는 broker redelivery가 아니라 Run/Job retry 정책으로 처리한다.

Trigger의 `delivery_id`는 canonical idempotency key로 정규화한다. Broker message ID, schedule occurrence ID, webhook delivery ID가 없으면 adapter가 안정적인 ID를 생성하고 재전달에서 보존해야 한다. Exactly-once delivery는 보장하지 않는다.

### 8. v0.3.0 한 번의 coordinated breaking release로 이전한다

현재 기준 버전은 pre-1.0인 v0.2.0이고 알려진 production 소비자는 모두 소유자가 확인된 저장소에 있다. 장기간 compatibility layer를 운영하지 않고 v0.3.0에서 canonical Invocation API, principal scope, Trigger SPI와 replacement SDK를 도입하는 동시에 `/execution/v1`, `/api/v1/w/{workspace}/run/...`, `/api/w/{workspace}/jobs/run/...`과 `/api/w/{workspace}/jobs/webhook/...` submission route를 제거한다.

Core와 downstream은 독립적으로 production에 순차 배포하지 않는다. Core release candidate와 Gale, Imprun gateway, wf-triggers migration branch를 같은 contract fixture로 사전 검증하고, maintenance window에서 ingress drain, Core v0.3.0, downstream consumer, live probe 순으로 전환한다.

Cutover gate는 모든 known consumer migration commit과 CI 준비, Core release candidate contract test, deploy manifest/image pin 준비, 이전 Core와 downstream release set으로의 rollback 절차 검증이다. 하나라도 준비되지 않으면 전체 cutover를 연기하며 일부 legacy handler만 임시 유지하지 않는다.

### 9. SDK도 Invocation 계약으로 이전한다

Canonical Python distribution과 module은 `windforce-invocation`과 `windforce_invocation`, client type은 `WindforceInvocationClient`로 한다. 기존 `windforce-execution` distribution과 `windforce_execution.WindforceExecutionClient`는 v0.3.0에서 legacy route와 함께 제거하며 compatibility import layer를 제공하지 않는다.

## Consequences

- 모든 caller와 trigger가 같은 release resolution, authorization, idempotency와 Run lifecycle을 사용한다.
- Public 여부와 trusted 여부를 URL namespace가 아니라 principal scope로 표현하므로 새 caller 종류 때문에 API plane을 추가하지 않는다.
- Run과 Job의 경계가 안정되어 worker queue 구현을 바꾸거나 attempt 모델을 확장해도 외부 Run contract를 유지할 수 있다.
- Gale, Imprun gateway, wf-triggers와 SDK를 함께 이전해야 하므로 v0.3.0은 maintenance window와 release-set rollback이 필요한 cross-repository breaking release가 된다.
- Legacy dhworker trigger처럼 임의 `env`와 caller-supplied client identity를 사용한 adapter는 canonical API에 그대로 옮길 수 없다. 해당 adapter는 새 `wf-triggers` 경계로 이전하거나 pinned legacy engine에 동결해야 한다.
- Trigger credential storage, protocol lifecycle, delivery observability와 dead-letter 운영이 Core의 self-hosted execution 범위에 추가된다.

## Rejected alternatives

- `/execution/v1`과 `/api/v1`을 영구적으로 유지하면 같은 admission에 두 SDK, 두 인증 정책과 두 기능 행렬이 남으므로 거부한다.
- 모든 caller를 `/execution/v1`으로 옮기면 client least privilege와 self-hosted public invocation 경계가 사라지므로 거부한다.
- Built-in trigger가 Core의 HTTP endpoint를 loopback 호출하면 같은 프로세스에서 불필요한 network/auth failure mode가 생기므로 거부한다.
- Nuclio처럼 broker ACK를 Action 처리 완료까지 미루면 장시간 Windforce Run이 broker delivery와 연결되고 engine retry와 broker retry가 중복되므로 거부한다.
- 두 minor release에 걸친 compatibility layer는 모든 known consumer를 함께 변경할 수 있는 pre-1.0 단계에서 중복 handler와 SDK를 더 오래 유지하므로 거부한다.
