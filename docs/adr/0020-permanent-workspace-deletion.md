# ADR 0020: Workspace 영구 삭제는 저장소 경계 전체를 원자적으로 제거한다

- Status: Accepted
- Date: 2026-08-02

## Context

Core는 workspace를 생성하고 보관할 수 있지만 영구 삭제는 제공하지 않았다. 보관은 실행과 변경을 차단하면서 기록을 유지하므로 운영 보존에는 적합하지만, 로컬 개발에서 잘못 만들었거나 더 이상 필요 없는 workspace와 그 데이터를 정리하는 수단은 아니다.

Workspace 데이터는 registry 한 행에만 있지 않다. Run과 Job, release catalog, trigger와 HTTP route binding, webhook outbox, variable과 resource, input config, credential, 암호화 key 및 audit가 같은 workspace 경계를 공유한다. Registry만 삭제하면 고아 데이터가 남고 local snapshot은 남은 데이터로 workspace를 다시 복원할 수 있다.

## Decision

1. Instance administrator 전용 `DELETE /api/workspaces/{workspace}`를 제공한다.
2. `default` workspace는 영구 namespace이므로 삭제할 수 없다.
3. 삭제는 registry와 모든 workspace-scoped 데이터를 한 저장소 transaction에서 제거한다. Local store는 snapshot 전체를 한 번에 교체하고 PostgreSQL store는 한 database transaction 안에서 dependent row를 먼저 제거한다.
4. Run은 직접 workspace column을 갖지 않으므로 Job payload의 canonical workspace를 기준으로 대상 Run, HumanTask, event와 log를 함께 제거한다.
5. 삭제 자체의 workspace audit tombstone은 남기지 않는다. 영구 삭제의 의미에 해당 workspace의 credential, encryption key, audit까지 모두 제거하는 것이 포함되기 때문이다. 외부 host가 별도 규정상 감사 기록을 요구하면 Core workspace 안이 아니라 host control plane에서 기록한다.
6. Web UI는 **Settings → Workspace → Lifecycle**에서 workspace 표시 이름을 정확히 입력한 뒤 삭제를 확인한다. 성공하면 활성 workspace를 `default`로 전환한다. `/ui/workspaces`는 계속 create/switch 전용 registry로 유지한다.

## Consequences

- 삭제는 되돌릴 수 없으며 보존이 필요하면 먼저 archive를 선택해야 한다.
- Local snapshot과 PostgreSQL이 같은 삭제 계약을 제공한다.
- 새 workspace-scoped table이나 snapshot collection을 추가할 때 영구 삭제 경계와 회귀 test도 함께 갱신해야 한다.
- 삭제 후 같은 ID로 새 workspace를 만들 수 있지만 이전 workspace의 기록이나 credential과 이어지는 복구 또는 동일성은 제공하지 않는다.
