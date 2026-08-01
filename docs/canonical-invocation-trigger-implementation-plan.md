# Canonical Invocation과 Trigger 구현 계획

- 상태: Phase 0~5 완료 — `v0.3.1` stable release, GitOps 배포와 canonical live probe 완료
- 결정 문서: [ADR 0013](adr/0013-canonical-invocation-and-trigger-boundary.md)
- Master tracking: [#137](https://github.com/imprun/windforce-core/issues/137)
- Phase 1 tracking: [#139](https://github.com/imprun/windforce-core/issues/139)
- 기준 버전: `v0.2.0` (`a92a42f`)

이 문서는 `/api/v1`을 canonical Invocation API로 만들고 Trigger admission 경계를 도입하는 구현 순서, cross-repository 소유권, coordinated big-bang cutover와 rollback gate를 정의한다. 단계별 schema와 route는 ADR 0013을 좁힐 수 있지만 그 결정과 다른 실행 plane, caller-controlled identity 또는 queue 직접 쓰기를 추가할 수 없다. OpenAPI와 JSON example은 시스템 간 통신 규격의 기계 판독 가능한 정본이다.

## 현재 구현 상태 (2026-08-01)

- Core Phase 1~3은 PR [#141](https://github.com/imprun/windforce-core/pull/141),
  [#145](https://github.com/imprun/windforce-core/pull/145),
  [#149](https://github.com/imprun/windforce-core/pull/149)로 완료됐다. Canonical Run API,
  principal/scope, legacy route 제거, `windforce-invocation` SDK와 Trigger SPI가 `main`에 있다.
- Gale은 PR [#37](https://github.com/imprun/gale/pull/37)에서 canonical Run API로 전환했고,
  PR [#79](https://github.com/imprun/gale/pull/79)에서 README 예제를 ADR 0023의 현재 규격과
  일치시켰다.
- Imprun Cloud gateway는 PR [#70](https://github.com/imprun/imprun/pull/70)에서 canonical
  tenant Run API로 전환했고 PR [#71](https://github.com/imprun/imprun/pull/71)로 배포됐다.
- 비운영 `data-team/dhworker/wf-triggers`는
  `data-team/dhworker/windforce-invocation-adapter`로 이름과 책임을 명확히 바꾸고 MR `!1`에서
  canonical `/runs/wait`, Run ID와 idempotency 규격을 구현했다. Legacy
  `data-team/dhworker/triggers`는 별도 pinned engine 대상으로 동결한다.
- Core stable release는 `v0.3.1` (`322ffbdd22ac31e86d3f493b108bd9e28aadc44d`)이며
  서명된 checksum, Linux/macOS/Windows host smoke와 image publication을 통과했다. Cell OCI
  index는 `sha256:8e1f7193014fd9427d995b32e7afabfa4ca56f57f4d479e50cfbf45f68e79d22`다.
- Imprun Cloud는 GitOps revision
  `c59b91d69e466a7ba56f5a408cdf5bb2ea00db56`에서 해당 digest를 고정했다. Control Plane
  image는 `sha256:e1366f3c9748d208b98af066f28f361507db48f3741458d5bc078d55b4bdc70f`다.
- Gale canonical migration commit은 `200449e140b6bd4265879cf927ded6b46446cbb9`이고, Cloud의
  active Gale release는 이 migration을 포함하는 `e1c3c800961e90dc21dbe23bd91459fd78439553`다.
- demo와 Gale Cell은 모두 새 digest에서 첫 기동 `Ready=true`, restart `0`을 기록했다.
  Gale Cell의 health/readiness와 scoped Service Principal `gale/health` wait/replay probe는
  HTTP 200과 동일 Run ID를 반환했다. 임시 principal은 즉시 폐기하고 삭제했다.

## 목표와 완료 조건

다음 여섯 결과가 모두 충족되어야 master issue를 완료한다.

| 결과 | 완료 조건 |
|---|---|
| Canonical 외부 API | Run create/wait/status/result/cancel과 app 규격 조회가 `/api/v1/workspaces/{workspace}`에서 operator, client, service principal 모두에 대해 검증된다. |
| Run/Job 경계 | Canonical response와 SDK는 Run ID만 노출하고 Job ID는 worker/control 진단 규격에만 남는다. |
| Principal 인증 | Caller identity가 credential에서 파생되고 scope와 ownership test가 있으며 body의 `client_id`, `env`, actor spoofing이 거부된다. |
| Trigger 확장성 | Built-in adapter가 공통 Trigger SPI와 AdmissionService를 사용하고 external adapter는 service principal로 같은 Invocation API를 사용한다. |
| Delivery 의미 | RabbitMQ integration test가 durable admission 뒤 ACK, retryable admission failure 시 requeue, terminal rejection 시 configured dead-letter 동작과 replay idempotency를 증명한다. |
| Legacy 제거 | 모든 known consumer migration과 release candidate가 사전 검증된 뒤 v0.3.0 한 번의 breaking cutover에서 legacy route와 SDK가 canonical API 도입과 함께 제거된다. |

## 소비자 인벤토리

| 소유 저장소 | 계획 수립 당시 통신 규격 | 근거 | 이전 조치 | 제거 gate |
|---|---|---|---|---|
| `imprun/windforce-core` | `/execution/v1`, `/api/v1/w/.../run`, `/api/w/.../jobs/run`, Python execution SDK | server handlers, OpenAPI, tests, CLI, README | Canonical handler/OpenAPI/SDK를 추가하고 기존 handler와 SDK를 같은 breaking release에서 제거 | Release candidate에 legacy route/OpenAPI/import가 없고 canonical 통신 규격 test 통과 |
| `imprun/gale` | `/execution/v1/workspaces/{workspace}/runs`와 lifecycle, ADR 0023 | `src/gale/adapters/windforce.py`, `docs/adr/0023-use-windforce-canonical-run-api.md` | `/api/v1` service principal과 `Idempotency-Key` header로 이전하고 ADR 갱신 | Gale main test와 submission-uncertain reconciliation test 통과 |
| `imprun/imprun` | Tenant gateway가 `/t/{tenant}/execution/v1/...` 전달, cell admin token 사용 | `internal/gateway`, `e2e/smoke_test.go`, decision ledger ADR-0003 | Tenant canonical prefix와 service credential custody로 이전하고 지원 engine version pin 갱신 | Gateway/E2E, Flux 배포와 tenant live probe 통과 |
| `data-team/dhworker/wf-triggers` (현재 `windforce-invocation-adapter`) | 비운영 구현이 `/api/v1/w/{workspace}/run/{app}/{action}/wait`, `wfk_` token, `X-WF-Job-Id`를 참조 | 기존 Python 구현과 README/tests; 운영 배포 없음 | 독립 Go protocol adapter에서 canonical Run wait endpoint, `X-WF-Run-Id`와 Run ownership을 구현 | Core 운영 cutover gate가 아님. 첫 배포 전에 dhworker envelope, timeout/idempotency test를 통과 |
| `data-team/dhworker/triggers` | Pinned `windforce-lite` execution SDK, caller `client_key`, per-job `env` | `pyproject.toml`, `src/dhworker_triggers/windforce.py` | 새 `wf-triggers`로 이전하거나 현재 pinned engine에 동결한다. Canonical Core에 `env`/identity impersonation을 다시 추가하지 않는다. | Windforce Core v0.3 cutover의 gate가 아니며 owner의 retire/migration 결정을 기록 |
| `imprun/windforce-lite`와 오래된 `C:\\Users\\USER\\WORK\\gale` checkout | 과거 통신 규격 사본 | source/doc references | Owning upstream main만 이전하고 오래된 checkout이나 vendor 사본을 정본으로 편집하지 않는다. | 정본 repository main 기준으로만 판단 |

`lamina`, `identity`, `tessera`와 나머지 현재 Imprun child repository에서는 `/execution/v1` 소비가 발견되지 않았다. 새 소비자가 발견되면 master issue의 inventory에 추가하고 v0.3 cutover를 연기한다.

## 목표 시스템 간 통신 규격

### Canonical routes

```text
POST /api/v1/workspaces/{workspace}/runs
POST /api/v1/workspaces/{workspace}/runs/wait?timeout={duration}
GET  /api/v1/workspaces/{workspace}/runs/{run_id}
GET  /api/v1/workspaces/{workspace}/runs/{run_id}/result
POST /api/v1/workspaces/{workspace}/runs/{run_id}/cancel
GET  /api/v1/workspaces/{workspace}/apps/{app}
GET  /api/v1/openapi.json
```

### Canonical create request

```json
{
  "app": "gale",
  "action": "parse",
  "input": {},
  "correlation_id": "document-123"
}
```

`Idempotency-Key`는 header로 전달한다. Principal, client/service ID, permissioned actor와 trigger kind는 인증 또는 registered Trigger에서 파생한다. Canonical request는 `env`, `client_id`, `created_by`, `permissioned_as`를 받지 않는다.

### Canonical asynchronous response

```json
{
  "run_id": "run_...",
  "state": "queued",
  "replayed": false
}
```

새 admission은 `201`, replay는 `200`이다. 응답은 `Location: /api/v1/workspaces/{workspace}/runs/{run_id}`와 `X-WF-Run-Id`를 포함한다. Wait timeout은 `202`와 현재 Run representation을 반환하고 완료된 wait는 `X-WF-Run-Id`와 raw Action result를 반환한다.

## 구현 단계

### Phase 0 — 통신 규격과 추적

- [x] 실제 `/execution/v1`, `/api/v1`과 control run route 소비자를 조사한다.
- [x] Run과 Job의 현재 모델 관계를 확인한다.
- [x] ADR 0013에서 canonical route, principal, Trigger와 delivery 결정을 확정한다.
- [x] Master issue #137에 release train과 known consumers를 등록한다.
- [x] ADR과 이 구현 계획을 main에 병합한다.

### Phase 1 — v0.3.0 canonical Invocation foundation

1. AdmissionService interface를 분리하고 기존 `execution.Service.CreateRun` caller를 같은 implementation으로 수렴시킨다.
2. Canonical Run handler와 OpenAPI를 추가하고 create/replay/wait/status/result/cancel/app 통신 규격 test를 작성한다.
3. Operator, client와 service principal authenticator를 공통 principal model로 수렴시키고 ownership/scope policy를 별도 authorization 단계로 둔다.
4. `/api/w/{workspace}/service-principals`에서 `wfs_` credential을 생성·회전·revoke하고 raw token one-time display와 hash-only persistence를 검증한다.
5. Canonical request에서 identity와 `env` spoofing을 거부하고 InputConfig와 Action schema resolution이 모든 principal에서 동일함을 검증한다.
6. Canonical metrics를 principal kind, result class, replay, wait outcome 단위로 기록하되 workspace 외 cardinality가 큰 ID, token과 input을 label로 넣지 않는다.

완료 gate는 `make fmt`, `make build`, `make test`, canonical OpenAPI snapshot/contract test와 PostgreSQL idempotency test 통과다.

### Phase 2 — v0.3.0 legacy 제거와 SDK 교체

- [x] `windforce-invocation`/`windforce_invocation.WindforceInvocationClient`를 canonical SDK로 추가한다.
- [x] Control CLI와 embedded examples를 canonical route와 Run ID로 이전한다.
- [x] `/execution/v1` handler/OpenAPI, `/api/v1/w/.../run[/wait]`, `/api/w/.../jobs/run[/wait]`과 `/api/w/.../jobs/webhook` submission handler를 삭제한다.
- [x] Legacy-only request/view code, `job_id`, `X-WF-Job-Id` 외부 응답과 execution SDK package를 삭제한다.
- [x] `/api/w`의 operator Run/Job 조회·로그·강제 취소와 `/worker/v1`은 유지하되 새로운 Run admission을 제공하지 않음을 통신 규격 test로 고정한다.
- [x] README, architecture, concepts와 generated OpenAPI에서 legacy 경로와 별도 Public/Execution plane 설명을 제거한다.

구현 추적: issue #142. Phase 2는 Core 저장소와 canonical SDK까지를
안정화하며 Gale, Imprun gateway, dhworker의 소비자 이전이나 배포는 각 저장소의
후속 phase가 소유한다.

완료 gate는 release candidate에 legacy route, Execution OpenAPI와 `windforce_execution` import가 존재하지 않고 canonical JSON/OpenAPI/SDK에 Job ID가 노출되지 않는 것이다.

### Phase 3 — Trigger resource와 SPI

완료 추적: [#146](https://github.com/imprun/windforce-core/issues/146)

- [x] Trigger definition, protocol config, enabled 상태, target와 credential reference의 storage/API/audit를 구현한다.
- [x] `Trigger`, lifecycle registry, `TriggerEvent`와 `TriggerSubmitter` interface를 구현하고 server/standalone 시작·종료에 연결한다.
- [x] Generic configured webhook adapter를 첫 구현으로 추가해 secret 검증, safe header/raw payload capture, delivery ID와 AdmissionService 직접 호출을 검증한다.
- [x] Open issue [#127](https://github.com/imprun/windforce-core/issues/127)의 schedule trigger를 같은 SPI 위에 구현하고 occurrence ID를 idempotency key로 사용한다.
- [x] RabbitMQ adapter를 추가해 reconnect, prefetch/concurrency, durable admission ACK, retryable NACK/requeue, terminal dead-letter와 shutdown drain을 integration test로 검증한다.
- [x] External trigger authoring guide에 service principal, canonical request, replay, timeout과 error classification을 문서화한다.

Built-in trigger test는 server의 HTTP listener를 거치지 않았음을 injectable fake AdmissionService로 증명한다. RabbitMQ test는 실제 broker container를 사용하되 CI에서 재현 가능한 topology와 bounded timeout을 사용한다.

### Phase 4 — Known consumer migration 준비

- [x] `imprun/gale` adapter와 ADR 0023을 canonical Run API와 service principal로 이전한다. 기존 `EXECUTION_SUBMISSION_UNCERTAIN`, tenant-scoped idempotency와 reconciliation 의미는 유지한다.
- [x] `imprun/imprun` gateway, decision ledger, console guide와 E2E를 canonical tenant prefix로 이전한다. Cell credential custody는 admin token 대신 least-privilege service token을 기본으로 하고 control 작업용 credential과 분리한다.
- [x] 비운영 `data-team/dhworker/windforce-invocation-adapter`를 canonical wait와 `X-WF-Run-Id` 기준으로 구현하고 기존 dhworker response envelope의 `TRACEID`에는 Run ID를 넣는다.
- [x] Core control CLI, README, architecture, concepts와 generated OpenAPI를 current canonical 통신 규격으로 갱신한다.
- [x] `data-team/dhworker/triggers`는 pinned legacy engine 대상으로 동결하고 별도 adapter 전환을 기록한다. 이 adapter 때문에 canonical API에 arbitrary `env`나 identity impersonation을 추가하지 않는다.

각 repository는 자신의 branch, test, CI와 배포를 소유한다. `windforce-core` PR 하나에 다른 child repository 변경을 섞지 않는다. 운영 Consumer migration PR은 Core v0.3.0 release candidate OpenAPI와 JSON fixture를 사용해 merge 전에 검증하되 production에는 cutover window 전 배포하지 않는다. `windforce-invocation-adapter`는 운영 전환 대상이 아니므로 안정된 규격을 소비하는 별도 첫 배포 절차를 따른다.

### Phase 5 — stable release와 pre-production cutover

- [x] Core, Gale과 Imprun gateway의 canonical commit, CI, stable release, image digest와 GitOps revision을 변경 기록에 고정한다.
- [x] 이 환경은 사용자 트래픽이 없는 pre-production 상태였으므로 승인된 big-bang 전환으로 수행하고 ingress drain/resume 단계는 적용하지 않는다.
- [x] Windforce Core `v0.3.0`을 배포한 뒤 rollout에서 발견된 LocalStore crash-recovery 결함을 #94와 PR #180으로 수정해 `v0.3.1`을 배포한다.
- [x] Imprun gateway와 Gale active release가 준비된 canonical migration commit을 포함하는지 확인한다.
- [x] 전체 lifecycle/authorization은 exact-SHA Core CI로, 실제 배포는 Cell health/readiness와 scoped `gale/health` wait/replay/result probe로 검증한다.
- [x] demo와 Gale Cell이 새 digest에서 restart `0`으로 안정화되고 lock-timeout log가 없음을 확인한다.

계획 당시의 `v0.2` 전체 rollback set은 운영에 진입한 적이 없고 Gale/Cloud canonical consumer와
호환되는 배포 집합도 아니므로 실제 cutover gate에서 제외했다. 직전의 완전한 canonical checkpoint는
GitOps `c767dfde3164569b4fd7c810aba354311859b1f5`와 Core `v0.3.0` OCI
`sha256:db7ca72c9a5d5363c48d879928ee025c36c436eeccd8f17ac34adf4f208e8a23`다.
Legacy handler를 되살리는 rollback은 허용하지 않으며, canonical release set 단위로 되돌리거나
검증된 patch로 roll-forward한다.

## Cross-repository 순서

다음 순서는 실제 사용자 트래픽이 있는 환경의 후속 전환 절차다. 이번 pre-production 최초
canonical 배포에서는 drain과 resume 단계가 적용되지 않았다.

```text
OpenAPI/JSON fixture와 Core v0.3 release candidate
  -> Gale, Imprun gateway migration branch 병렬 검증
  -> rollback release set 고정
  -> invocation ingress drain
  -> Core v0.3 breaking deployment
  -> consumer release set deployment
  -> canonical live probes
  -> ingress resume

비운영 `wf-triggers`는 위 운영 release set과 별도로, 안정된 Core v0.3 OpenAPI를 기준으로 구현·검증한 뒤 첫 배포한다.
```

Core와 downstream의 production 전환은 같은 maintenance window에 수행한다. Imprun gateway가 지원하는 Core image pin은 새 gateway와 cell image가 함께 준비된 release set에서만 올린다.

## 검증 matrix

| 영역 | 필수 검증 |
|---|---|
| Admission | 같은 principal과 idempotency key가 response loss/retry 뒤 같은 Run ID를 반환 |
| Authorization | operator/client/service scope, workspace mismatch, own/any ownership, archived workspace |
| Input security | LockedKeys, schema validation, `env`/identity spoof rejection, body/token size 제한 |
| Lifecycle | create, wait completion, wait timeout, status, result, cancel, replay |
| Model boundary | Canonical JSON/OpenAPI/SDK에 Job ID와 lease/attempt가 없음 |
| Breaking removal | Legacy route와 Execution OpenAPI가 404이고 legacy SDK import가 release artifact에 없음 |
| Trigger | start/stop, enable/disable, credential redaction, no loopback HTTP |
| RabbitMQ | ACK after admission, NACK retry, dead-letter terminal error, duplicate delivery replay |
| Observability | principal/route class metric, audit subject, secret/input 비기록 |
| Downstream | Gale uncertainty reconciliation, Imprun tenant gateway E2E; 비운영 wf-triggers는 첫 배포 전 envelope 검증 |

## Rollback과 데이터 변경 원칙

Big-bang 전환의 rollback 단위는 Windforce Core, Imprun gateway와 Gale의 고정된 canonical
release set 전체다. 일부 consumer만 legacy로 되돌리거나 제거된 handler를 production hotfix로
복원하지 않는다. 비운영 `windforce-invocation-adapter`는 rollback 단위가 아니다.

Service principal과 Trigger storage migration은 additive로 유지한다. Legacy HTTP/SDK code는 제거하지만
기존 Run/Job column을 같은 release에서 destructive하게 삭제하지 않는다. `v0.2.0` Core artifact
(`sha256:2218427b247a13539af51d43f6685094ea8f6a658e28a58ce11211c6508a071c`)는 보존하지만,
canonical Gale/Cloud가 배포된 뒤에는 단독 runtime rollback 대상으로 사용하지 않는다. Data cleanup은
cutover 안정화 뒤 별도 migration으로 다룬다.

Canonical admission 자체에 문제가 생기면 공통 AdmissionService와 consumer release set을 함께 rollback한다. 두 admission implementation을 동시에 운영하는 임시 수정은 금지한다.

## 구현 issue 분할

Master issue #137 아래에서 최소 다음 단위로 나눈다.

1. Canonical Run API, OpenAPI와 principal authorization.
2. Service principal registry와 token lifecycle.
3. Legacy route/SDK 제거와 canonical invocation SDK.
4. Trigger resource, SPI와 configured webhook.
5. Schedule trigger #127과 RabbitMQ adapter.
6. Gale과 Imprun gateway의 coordinated migration, wf-triggers의 별도 첫 구현.
7. Coordinated cutover, live evidence와 rollback rehearsal.

각 issue는 앞 단계의 canonical 시스템 간 통신 규격을 재정의하지 않는다. Breaking schema 또는 delivery semantic 변경이 필요하면 ADR 0013을 먼저 갱신한다.
