# ADR 0014: Workspace lifecycle과 named credential을 분리한다

- Status: Accepted
- Date: 2026-07-27

## Context

기존 Core는 workspace 생성 응답에서 단일 `wfw_` token을 함께 발급하고
`POST /api/workspaces/{workspace}/token`으로 그 값을 교체했다. 이 모델은
workspace와 credential의 수명주기를 하나로 묶는다. 최초 secret을 잃어버린
운영자는 기존 credential의 이름, 사용 목적, 상태를 확인할 수 없고, 여러 CLI나
복구 경로 중 하나만 선택적으로 회전하거나 폐기할 수도 없다.

Hosted 환경에서는 더 큰 경계 혼동도 생긴다. Host의 Cloud API token, 로그인된
browser session, Core가 발급한 workspace token이 모두 bearer 또는 proxy
delegation으로 Cell에 도달할 수 있지만 이들은 같은 credential이 아니다. Host
제어 API token을 Cell operator credential로 암묵 교환하면 token의 대상과 권한
범위가 넓어지고, Core audit actor가 실제 인증 principal 대신 임의의 browser
설정이나 일반 gateway 이름으로 기록될 수 있다.

UI도 workspace registry의 Manage 상세에 identity, access, audit, lifecycle을
모두 배치해 현재 선택된 workspace의 Settings와 경쟁하는 두 관리 위치를 만들었다.

## Decision

1. Workspace 생성은 workspace record만 만들며 credential을 자동 발급하지 않는다.
2. Workspace credential은 stable ID와 name을 갖는 별도 resource다. 한 workspace는
   여러 named `wfw_` credential을 가질 수 있다.
3. Raw secret은 create 또는 rotate 응답에서만 한 번 반환하고 저장소에는 SHA-256
   hash만 저장한다. Rotate는 해당 credential의 이전 secret만 즉시 무효화하며,
   revoke는 replacement 없이 해당 credential을 비활성화한다.
4. Instance administrator만 global `/api/workspaces/.../tokens` lifecycle을
   제어한다. Workspace principal은 계속 한 workspace의 operator 역할만 수행하며
   credential lifecycle이나 다른 workspace에 접근하지 못한다.
5. 기존 snapshot/PostgreSQL의 단일 token hash는 배포 migration에서
   `Legacy access token` named credential로 옮긴다. Raw value를 다시 만들거나
   기존 호출자를 끊지 않는다.
6. Workspace identity, access, lifecycle audit는 canonical workspace Audit에
   `workspace` category로 포함한다. 인증된 workspace token subject와 trusted
   host delegation subject가 임의의 `X-Windforce-Actor`보다 우선한다.
7. Core가 발급한 `wf` family bearer는 fronting proxy가 그대로 전달한다. Hosted
   browser session은 host가 검증한 principal로 위임할 수 있다. Host control-plane
   API token은 Core credential이 아니며 Cell hostname에서 자동 교환하지 않는다.
8. `/ui/workspaces`는 create/switch 전용 registry다. 활성 workspace의 identity와
   lifecycle은 Settings → Workspace, credential은 Settings → Access, history는
   통합 Audit에서 관리한다. 기존 detail URL은 새 위치로 redirect한다.

## API

- `GET /api/workspaces/{workspace}/tokens`
- `POST /api/workspaces/{workspace}/tokens`
- `POST /api/workspaces/{workspace}/tokens/{token}/rotate`
- `DELETE /api/workspaces/{workspace}/tokens/{token}`

Create와 rotate 응답만 `api_token`을 포함한다. List와 revoke 응답은 ID, name,
status, actor, timestamp metadata만 반환한다. 기존
`POST /api/workspaces/{workspace}/token`은 breaking change로 제거한다. Core는
운영 상태가 아니므로 dual-write나 장기 compatibility endpoint를 두지 않는다.

## Consequences

- Workspace 생성 직후에는 operator credential이 없으므로 필요한 credential을
  명시적으로 이름 붙여 발급해야 한다.
- 하나의 secret 유출이나 분실을 다른 caller의 중단 없이 처리할 수 있다.
- Database와 local snapshot schema에 credential collection 및 legacy migration이
  추가된다.
- Hosted platform은 Cloud API 인증과 Cell browser delegation을 구분해야 한다.
- Machine caller는 계속 `wfw_`, `wfk_`, `wfs_`, `wfr_`, `wfjob_` 계약을 사용하므로
  standalone 및 protocol adapter 경계는 유지된다.
