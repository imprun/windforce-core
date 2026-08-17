---
title: 제품 책임 경계
description: Windforce Core가 다중 언어·Provider Runtime 플랫폼이 아니라 Bun/TypeScript 실행 코어인 이유입니다.
---

Windforce Core는 내구성 있는 Script App을 위한 범용 실행·통합 코어입니다. 불변 App Release를 동기화·발행하고, Run을 admission하며, Job을 queue에 넣고, Worker Attempt를 fencing하고, Runtime 설정과 실행 제한을 적용하며, 선택적인 외부 capability를 연결합니다.

Core의 범용성은 모든 언어를 실행하거나 모든 인프라 서비스를 내장하는 데 있지 않고 이런 중립적인 실행 통신규격에 있습니다.

[English](../../concepts/product-boundary.md)

## 권장 App 경로와 호환성

| 경로 | 제품 상태 | 의미 |
| --- | --- | --- |
| Bun/TypeScript | Tier 1 | 새 App, Core Author SDK 기능, 예제, Application SDK 통합의 권장 경로입니다. |
| Python | 호환성 | 기존 Manifest, Bundle, Launcher 동작과 Author SDK 표면을 유지합니다. 새 기능의 동등 구현은 자동 요구사항이 아닙니다. |
| Go | 호환성 | 기존 Manifest, 정적 Bundle 준비, Launcher 동작과 Author SDK 표면을 유지합니다. 새 기능의 동등 구현은 자동 요구사항이 아닙니다. |
| Adapter command | 명시적 확장 | 배포별 실행 Adapter이며 새 내장 작성 언어나 암묵적 fallback이 아닙니다. |

Manifest 호환성을 위해 `scriptLang`을 생략하면 계속 `typescript`가 됩니다. Core는 구현된 명시적 값만 허용하며 알 수 없는 언어를 Bun으로 처리하지 않습니다. 이 제품 방향은 Python과 Go를 deprecation하지 않습니다. 제거하거나 호환성 약속을 줄이려면 별도 결정과 Migration이 필요합니다.

## 책임 경계

```text
Hosted 제품 또는 설치형 운영자
  -> 환경 정책, 인프라, Fleet capacity, Provider service

Windforce Core
  -> Source sync, 불변 Release, Admission, Queue, Lease/Fencing
  -> Retry, Cancellation, Limit, Runtime 설정, Placement
  -> Job 범위 Capability binding, Masking, Cleanup, Completion

Bun/TypeScript App
  -> Action orchestration과 Domain 동작
  -> 불투명한 Application SDK dependency

외부 Capability service
  -> Browser, GPU/AI, Document/Native engine, Mobile, Private connector
  -> Provider API, Native resource limit, Binary artifact, Provider error
```

Core는 Hosted Control Plane 없이도 완결됩니다. 설치형 사용자는 Embedded Worker로 일반 App을 실행하고, App이 필요로 할 때만 직접 운영하는 Capability service를 연결할 수 있습니다. Hosted 제품과 사내 Fleet도 같은 Core HTTP·Worker 통신규격을 사용하며 상용 Tenant나 Provider 용어를 Core에 추가하지 않습니다.

상용 요금제, 가격, 청구, 법적 계약, Hosted Tenant 수명주기는 Hosted 제품이 소유합니다. Core는 설치형 환경에도 의미가 있는 중립적인 실행 정책만 저장하고 강제합니다.

## Runtime mode가 아닌 Capability service

Browser Edge, GPU inference, Document engine 같은 시설은 추가 Core Runtime mode가 아닙니다. 독립적인 Scaling, Native dependency, 장기 실행 Process, 대용량 Binary 전송, Provider credential 또는 Hardware scheduling이 필요한 경우가 많습니다.

Core는 통합의 중립적인 실행 부분만 소유합니다.

- App 또는 운영자가 일반 Placement 요구사항을 선언합니다.
- 적합한 Worker가 설정된 Capability service를 탐색합니다.
- Core가 Worker credential로 Job 범위 Session을 엽니다.
- App은 Private Launcher transport를 통해 수명이 짧은 불투명 Binding만 받습니다.
- Core가 Binding을 masking하고 모든 종료 경로에서 닫습니다.
- Service가 Provider 호출, Artifact, 동시성, 정제된 Provider error를 소유합니다.

현재 Worker-local gateway 통신규격은 [ADR 0034](../../adr/0034-bind-worker-local-capability-gateways.md)에 정의합니다. 여러 Gateway나 Remote gateway 지원은 별도의 신뢰·Routing 결정이 필요하며 이 제품 경계에서 자동으로 허용되지 않습니다.

## 비목표

Windforce Core는 다음 제품이 아닙니다.

- 모든 프로그래밍 언어에 동등한 투자를 약속하는 Framework
- Browser, GPU, Mobile, Document 또는 Database service 구현
- Kubernetes, VM 또는 Worker fleet provisioning controller
- 상용 Tenant, Pricing, Billing 또는 Global quota system
- 장기 실행 Actor, Durable Object 또는 Object별 Database runtime

장기 실행 Named entity가 나중에 필요할 수 있지만 종료되는 Run/Job과는 Identity, Single writer, Routing, Storage, Migration, Recovery 의미가 다릅니다. 특수한 Job이나 Resource로 모델링하지 않고 구체적인 소비자와 별도 ADR이 필요합니다.

## 관련 통신규격

- [Core 개념 영문 문서](../../concepts/core-concepts.md)는 내구성 있는 Source, Release, Run, Job 모델을 정의합니다.
- [앱 런타임 통신규격과 SDK 책임 경계](app-runtime-interface.md)는 Application SDK를 Core에 불투명하게 유지합니다.
- [Worker 실행 수명주기](worker-execution.md)는 고정된 Job이 하나의 Process Attempt가 되는 절차를 정의합니다.
- [Runtime 설정과 Secret](runtime-configuration.md)은 Job 범위 Variable, Resource, InputConfig, Secret 해석을 정의합니다.
- [ADR 0046](../../adr/0046-define-bun-typescript-app-and-external-capability-boundary.md)은 결정과 기각한 대안을 기록합니다.
