---
title: 실행 제한
description: 한 Core Cell의 여러 Worker에 App 전체, 불투명 키 동시성, 불투명 키 고정 윈도우 rate 제한을 원자적으로 적용합니다.
---

Windforce Core는 함께 적용되는 세 단계의 실행 제한을 제공합니다.

- `maxConcurrent`는 한 workspace와 App에서 실행 중인 모든 Job 수를 제한합니다.
- `executionLimits.concurrency`는 App이 정의한 같은 불투명 키로 해석되는
  Job 수를 제한합니다.
- `executionLimits.rate`는 App이 정의한 같은 불투명 키의 claim 시도 수를
  UTC epoch 기준 고정 윈도우 안에서 제한합니다.

모든 제한은 Worker가 Job을 가져가는 시점에 적용됩니다. 제한에 걸린 Job은
큐에 남고 기다리는 동안 Worker 슬롯을 사용하지 않습니다.

## 키 제한 선언

App의 여러 Action이 용량을 공유해야 하면 App 수준에 `executionLimits`를
둡니다.

```json
{
  "app": "orders",
  "entrypoint": "main.ts",
  "executionLimits": {
    "concurrency": [
      {
        "id": "account-egress",
        "maxConcurrent": 2,
        "inputPointers": ["/account_id", "/egress/id"]
      }
    ],
    "rate": [
      {
        "id": "account-egress",
        "maxAttempts": 120,
        "windowSeconds": 60,
        "inputPointers": ["/account_id", "/egress/id"]
      }
    ]
  },
  "actions": {
    "collect": {}
  }
}
```

특정 Action만 제한하려면 원하는 선언을 그 Action 안에 둡니다. App과 Action
제한은 함께 적용되며 기존 `maxConcurrent`도 계속 적용됩니다.

`inputPointers`는 Core가 workspace, App, Action, client InputConfig를 병합한
유효 입력을 가리키는 RFC 6901 JSON Pointer입니다. 선택된 값은 문자열,
숫자 또는 boolean이어야 합니다. 값이 없거나 null, object, array이면
Admission을 거부하므로 의도치 않게 모든 Job이 한 버킷에 들어가지 않습니다.

## 요청과 claim 흐름

```mermaid
flowchart TD
    A["Invocation 요청"] --> B["Admission이 InputConfig 해석"]
    B --> C["Action 입력 스키마 검증"]
    C --> D["선언된 JSON Pointer 값 조회"]
    D --> E["스칼라 구성요소 정규화"]
    E --> F["Workspace 키로 HMAC"]
    F --> G["정책 ID, revision, digest, 예산과 윈도우를 Run과 Job에 고정"]
    G --> H["Local 또는 PostgreSQL 큐"]
    H --> I["동시성과 현재 rate 예산을 원자적으로 검사"]
    I -->|"모든 제한 허용"| J["rate 시도 소비, Worker lease 생성과 실행"]
    I -->|"제한에 걸림"| K["Job이 큐에 대기"]
```

Core는 제한 pin에 HMAC digest만 저장하고 인덱싱합니다. 선택한 원본 값은
제한 pin, 로그, 진단 정보에 저장하지 않습니다. 유효 Run 입력은 기존 입력
암호화 저장 규칙을 그대로 따릅니다.

권한이 있는 Job 상태 응답은 진단을 위해 정책 ID, revision, scope, digest,
최대값과 rate 윈도우로 구성된 안전한 `execution_limits` pin을 보여 줍니다.
선택된 원본 키 구성요소는 보여 주지 않습니다.

## 키 선택 방법

동시에 사용하면 안 되는 외부 자원을 식별하는 안정적인 필드를 사용합니다.
예를 들면 계정 ID와 egress 식별자의 조합입니다. 정책 ID는 namespace이므로
이전 Release와 새 Release의 Job이 용량을 공유해야 하면 ID를 유지합니다.

HMAC은 저장된 제한 상태에서 낮은 엔트로피 값을 추측하기 어렵게 하지만,
호출자가 자유롭게 바꿀 수 있는 필드에 권한을 부여하지는 않습니다. 이 제한을
보안 quota로도 사용할 때는 신뢰할 수 있는 Admission 문맥에서 값을 공급하거나
운영자 InputConfig로 해당 값을 잠가야 합니다.

## 생명주기 동작

- 실행 중인 lease가 용량을 사용합니다.
- 완료하거나 만료된 lease를 회수하면 용량이 반환됩니다.
- claim 성공 시 rate 시도 한 건을 소비합니다. 완료, 실패, 취소, lease 만료,
  retry, recovery는 소비한 시도를 반환하지 않습니다.
- retry 또는 복구된 Job은 다시 claim될 때 rate 시도를 하나 더 소비합니다.
- HumanTask hold는 프로세스와 lease를 유지하므로 용량을 계속 사용합니다.
- suspend와 retry는 Run에 저장된 안전한 pin을 다시 사용합니다.
- queue-demand 관측은 동시성 제한에 막힌 Job과 현재 윈도우의 남은 rate
  예산을 초과하는 시도를 증설 수요에서 제외합니다.

## 고정 윈도우 동작

`windowSeconds: 60`이면 Core는 UTC epoch 경계에 맞춘 1분 윈도우로 시도를
묶습니다. 다음 경계에서 예산이 초기화되며 한 서버 프로세스의 타이머에
의존하지 않습니다. Local 모드는 Store에 주입된 시계를, PostgreSQL 모드는
claim transaction 안의 데이터베이스 시계를 사용합니다.

고정 윈도우는 경계 직전과 직후를 합쳐 설정값의 약 두 배에 가까운 burst를
허용할 수 있습니다. 이는 v1의 명시적인 절충입니다. 이 계약이 sliding window나
token bucket 의미를 암묵적으로 제공하지는 않습니다.

## 경계

원자성의 범위는 한 Core Cell과 그 데이터베이스입니다. Hosted control plane은
여러 Cell을 아우르는 전역 quota와 WorkerPool 운영을 소유합니다. 성공률과 대상
상태에 따른 제출 판단은 도메인 서비스가 소유합니다.

Rate 제한은 지속되는 시도 bucket을 사용하며 동시성 counter를 재사용하거나
반환하지 않습니다. Rate는 실행 안전 primitive이지 상용 청구나 여러 Cell을
아우르는 quota 시스템이 아닙니다.

`/metrics`의 `windforce_execution_rate_claims_total` counter는 Store
backend와 `consumed` 또는 `blocked` 결과만 보고합니다. workspace, App, 정책,
불투명 키 식별자는 label로 사용하지 않습니다.

키 동시성은 [ADR 0033](../../adr/0033-pin-and-enforce-opaque-key-concurrency.md),
고정 윈도우 rate 의미와 실패 동작은
[ADR 0041](../../adr/0041-pin-and-enforce-opaque-key-fixed-window-rate.md)을 참고하십시오.
