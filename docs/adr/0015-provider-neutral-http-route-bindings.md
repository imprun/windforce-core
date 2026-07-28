# ADR 0015: HTTP Trigger와 외부 HTTP Route Binding을 분리한다

- Status: Accepted
- Date: 2026-07-28
- Issue: [#152](https://github.com/imprun/windforce-core/issues/152)

## Context

Windforce Core의 webhook Trigger는 다음 canonical ingress로 event를 받는다.

```text
POST /api/v1/workspaces/{workspace}/triggers/{trigger}/events
```

이 경로는 Trigger 인증, delivery idempotency와 Run admission의 정본이다. 하지만
Kubernetes나 hosted platform의 사용자는 별도 hostname과 짧은 public path를
통해 webhook을 노출하려 한다.

Nuclio는 HTTP Trigger 설정의 ingress host/path를 Kubernetes controller가
Ingress resource로 변환한다. 이 접근은 함수와 외부 route의 lifecycle을 함께
관리한다는 장점이 있지만, Windforce Core가 Kubernetes API 타입, cluster
credential과 ingress implementation을 직접 소유하면 standalone 및 다른
orchestrator에서 같은 Trigger 모델을 사용할 수 없다.

Imprun Cloud는 이미 `*.cloud.imprun.dev`를 하나의 Cloud Gateway로 전달한다.
따라서 hosted cell의 Trigger마다 cluster-scoped `HTTPRoute`를 생성할 필요가
없다. Cloud Gateway가 tenant와 workspace를 확인한 뒤 canonical ingress로
rewrite할 수 있다.

## Decision

### 1. Trigger와 HTTP Route Binding을 별도 resource로 둔다

Trigger는 protocol event를 Run admission으로 연결하는 설정과 lifecycle을
소유한다. HTTP Route Binding은 webhook Trigger의 canonical ingress를 외부
hostname/path에 연결하려는 desired state와 provider가 보고한 observed state를
소유한다.

```text
external request
  -> Router Provider
  -> canonical webhook ingress
  -> Trigger
  -> AdmissionService
  -> Run + Job
```

Route Binding이 없어도 canonical ingress는 항상 유효하다. Route Binding 삭제나
provider 장애가 Trigger definition과 canonical ingress를 삭제하지 않는다.

### 2. Core는 provider-neutral desired/observed state만 소유한다

Portable desired fields는 다음과 같다.

- `hostname`: 외부 hostname. 비어 있으면 provider가 기본 hostname을 선택할 수
  있다.
- `path`: `/`로 시작하는 외부 request path.
- `visibility`: 현재 `public`만 지원한다.
- `provider`: `auto` 또는 operator가 선택한 provider name.

Observed fields는 provider가 기록한다.

- `state`: `pending`, `ready`, `error`, `deleting`, `deleted`
- `public_url`
- `error_summary`
- `observed_generation`

Desired field가 변경될 때 `generation`이 증가하고 state는 `pending`으로
돌아간다. `ready`는 `observed_generation == generation`일 때만 현재 desired
state에 대한 준비 완료를 뜻한다.

Core state와 Control API에는 Kubernetes `Gateway`, `HTTPRoute`, `Ingress`,
namespace, listener나 certificate issuer 같은 provider-specific field를
추가하지 않는다.

### 3. Router Provider SPI는 Control API 기반 reconciliation contract다

Provider/controller는 workspace의 Route Binding desired state를 조회하고,
자신이 소유한 외부 route를 idempotent하게 reconcile한 뒤 observed state를
status endpoint로 보고한다.

```text
GET /api/w/{workspace}/http-route-bindings?include_deleted=true
PUT /api/w/{workspace}/http-route-bindings/{binding}/status
```

사용자와 UI가 Trigger 문맥에서 사용하는 resource API는 다음과 같다.

```text
GET    /api/w/{workspace}/triggers/{trigger}/routes
POST   /api/w/{workspace}/triggers/{trigger}/routes
GET    /api/w/{workspace}/triggers/{trigger}/routes/{binding}
PUT    /api/w/{workspace}/triggers/{trigger}/routes/{binding}
DELETE /api/w/{workspace}/triggers/{trigger}/routes/{binding}
GET    /api/w/{workspace}/triggers/{trigger}/routes/{binding}/audit
```

Provider status update는 desired fields를 바꿀 수 없다. Provider는 자신이 처리한
generation을 `observed_generation`으로 반드시 보낸다. 오래된 generation의
응답은 저장할 수 있지만 현재 binding을 `ready`로 만들 수 없다.

삭제 요청은 즉시 hard delete하지 않고 `deleting` tombstone을 남긴다. Provider가
외부 route 삭제를 확인해 `deleted`를 보고한 후 일반 목록에서 숨긴다. Provider는
`include_deleted=true`를 사용해 tombstone을 계속 관찰할 수 있다.

### 4. Provider availability는 capability로 공개한다

Core는 configured provider name을 system info의 capability로 공개한다. Provider가
없는 standalone 환경에서는 UI의 Public route section을 숨기고 canonical ingress
사용법만 보여준다. Route Binding API와 state model 자체는 유지되므로 desired
state를 미리 저장하거나 외부 controller가 나중에 reconcile할 수 있다.

Provider가 있다고 해서 route 생성 성공을 즉시 의미하지 않는다. UI와 API
consumer는 `pending`, `ready`, `error` observed state와 `public_url`을 표시해야
한다.

### 5. Kubernetes와 Imprun Cloud는 서로 다른 provider가 소유한다

- Self-hosted Kubernetes provider는 Gateway API `HTTPRoute` 또는 선택한 ingress
  implementation을 생성하고 canonical ingress service로 rewrite한다. 해당
  controller와 RBAC는 Core repository 밖에서 소유한다.
- Imprun Cloud provider는 기존 wildcard Cloud Gateway route 안에서 hostname/path
  alias를 등록하고 tenant/workspace authorization 후 해당 cell의 canonical
  ingress로 rewrite한다. Trigger마다 cluster `HTTPRoute`를 생성하지 않는다.
- Core는 두 provider의 credentials, certificate, DNS와 hosted tenant policy를
  알지 못한다.

### 6. 보안 경계는 canonical ingress에서 유지한다

Friendly public route는 canonical ingress와 같은 webhook signature 검증, body
size limit, rate limit, delivery idempotency와 admission policy를 거쳐야 한다.
Router Provider는 검증을 우회해 AdmissionService나 state store에 직접 쓰지
않는다.

Audit에는 desired create/update/delete와 provider status transition을 남기되
credential, signature와 raw provider error body를 기록하지 않는다.

## Consequences

- Webhook Trigger와 외부 ingress lifecycle을 함께 표현하면서도 Core의
  standalone/provider-neutral 성격을 유지한다.
- Provider 장애는 `pending` 또는 `error`로 관찰되며 canonical ingress는 계속
  동작한다.
- Kubernetes operator와 Imprun Cloud Gateway는 같은 Route Binding contract를
  소비하지만 각자의 route resource와 policy를 독립적으로 소유한다.
- 삭제는 비동기 reconciliation이므로 UI에 `deleting` 상태가 잠시 보일 수 있다.
- Provider controller는 authenticated Control API access와 generation-aware
  reconciliation을 구현해야 한다.

## Rejected alternatives

- Core가 Kubernetes client와 `HTTPRoute` 타입을 직접 포함하면 standalone,
  Docker와 다른 orchestrator가 Kubernetes implementation에 종속되므로
  거부한다.
- Trigger config에 `host`, `path`, `gateway`를 섞으면 protocol delivery와
  infrastructure lifecycle이 하나의 secret-bearing document에 결합되므로
  거부한다.
- Imprun Cloud에서 Trigger마다 cluster `HTTPRoute`를 생성하면 이미 존재하는
  wildcard Cloud Gateway와 route ownership이 중복되고 tenant policy가 Core로
  새므로 거부한다.
- Friendly route에서 곧바로 Run을 생성하면 Trigger signature, delivery
  idempotency와 audit 경계를 우회하므로 거부한다.
