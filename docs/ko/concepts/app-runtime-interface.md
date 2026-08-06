---
title: 앱 런타임 통신규격과 SDK 책임 경계
description: Core와 실행 가능한 App Bundle 사이의 SDK 중립적인 시스템 경계입니다.
---

이 문서는 Windforce Core와 실행되는 App 사이 통신규격의 현재 정본입니다. 여기서 interface 또는 contract는 법적 계약이 아니라 Runtime component, App, SDK 사이의 통신규격을 뜻합니다.

> Trace 구현 상태 (2026-08-06): 선택적 Telemetry Carrier는 ADR 0029에서 승인하고 GitHub issue #128에서 추적하는 목표 통신규격입니다. 현재 Core Author SDK에는 아직 구현되지 않았습니다.

[English](../../concepts/app-runtime-interface.md)

## 핵심 원칙

Core는 App이 어떤 SDK를 사용하는지 탐지, 분류, import 또는 협상하지 않으며 관심을 가져서도 안 됩니다. App은 scraping SDK, Playwright, Puppeteer, Mobile SDK, 여러 SDK를 함께 사용하거나 SDK를 전혀 사용하지 않을 수 있습니다. Core에는 모두 같은 불변 App Bundle 안에 들어 있는 불투명한 App dependency입니다.

Core가 평가하는 것은 자신의 실행 통신규격뿐입니다.

- 고정된 Manifest와 준비된 Execution Bundle이 유효합니다.
- 선택한 Worker와 언어 Launcher가 호환됩니다.
- 고정된 Entrypoint가 `main(coreCtx)`를 제공합니다.
- Process가 Core 수명주기 규칙에 따라 Log와 최종 Result를 만듭니다.

Package 이름, SDK Context version, Module envelope, HTTP helper 형태와 Browser library는 Core의 Admission, Scheduling 또는 Execution 입력이 아닙니다.

## SDK 이름은 사람을 위한 설명이지 Core Runtime type이 아니다

생태계에서는 서로 다른 helper를 SDK라고 부릅니다. 다음 구분은 사람을 위한 문서 용어이며 Core가 SDK Registry를 만들거나 이 분류에 따라 실행을 분기한다는 뜻이 아닙니다.

| Helper | 실행 위치 | Core가 보는 것 |
| --- | --- | --- |
| Invocation SDK | Core 밖에서 `/api/v1`을 호출하는 선택적 HTTP client | Client library가 아닌 인증된 HTTP request |
| Core Author SDK | Core가 제공하는 언어 type과 helper로 App Bundle 내부에서 실행 | 동일한 `main(coreCtx)` 통신규격 |
| 임의의 Application SDK | 일반 dependency로 App Bundle 내부에서 실행 | SDK 관련 정보는 없으며 App process와 Result만 관찰 |

## Core에서 App으로 전달하는 통신규격

실행 가능한 App Bundle은 `windforce.json`에 선언한 Entrypoint와 Action을 제공합니다. 기본 TypeScript, Python, Go Runtime에서 App Entrypoint는 최종적으로 Core가 만든 Context 하나를 받고 Action 최종값 하나를 반환합니다.

```text
Core Worker
  -> 고정된 Deployment와 Execution Bundle 검증
  -> 고정된 언어 Launcher 선택
  -> claim한 Job의 Core Context 구성
  -> App main(coreCtx) 호출
  -> Log와 최종 Result 수집
  -> lease가 걸린 Job 완료
```

Core는 Host Context의 의미를 소유합니다. 여기에는 유효 입력값, Trigger metadata, App과 Action identity, Job 범위 identity, Actor metadata, Logger, Variable, Resource, State, low-level HTTP, Approval, Flow resume 값과 읽기 전용 선택적 W3C Telemetry Carrier가 포함됩니다. Core는 Runtime wrapper와 Author SDK가 이 기능을 구현할 때 사용하는 private transport도 소유합니다.

App 코드와 dependency는 Context 기능을 사용해야 합니다. Private `WF_*` 환경변수를 직접 해석하거나 `WF_TOKEN`을 전달하거나 Core callback URL을 만들거나 Queue record에 쓰거나 Worker Plane을 호출해서는 안 됩니다. Core가 소유하는 Launcher와 Author SDK glue만 private process transport를 public Context 표면으로 변환할 수 있습니다. 이 예외가 private transport를 App API로 만들지는 않습니다.

Application SDK는 Core Context가 노출하는 선택적 Telemetry Carrier를 이어서 사용하고 SDK, App 또는 Action Span을 만들 수 있습니다. 이 Carrier는 현재 Job 실행 Context이며 Worker Polling과 Worker Plane Transport Context는 App interface에 들어오지 않습니다. Core나 유효한 Carrier 없이 SDK를 직접 실행하면 SDK가 자체 Root Trace를 시작할 수 있습니다. Core는 SDK를 탐지하지 않으며 실행을 위해 추적을 요구하지 않습니다. [실행 관측성과 디버깅](execution-observability.md)과 [ADR 0029](../../adr/0029-optional-trace-context-continuity.md)를 참고합니다.

현재 TypeScript의 low-level HTTP 기능은 `coreCtx.http.fetch`입니다. Application SDK는 의도적으로 다른 작성 API를 제공할 수 있습니다. 예를 들어 scraping SDK는 `scrapingCtx.httpService.get()`과 `post()`를 제공할 수 있습니다. 이 메서드는 App process 안에서 Host 기능을 사용해 구현하므로 Core는 해당 메서드를 이해하거나 검사하지 않습니다.

## Application SDK Adapter

Application SDK는 불변 Source와 Execution Bundle에 포함되는 불투명한 App dependency입니다. App 경계에서 Core Context를 다른 Context로 변환할 수 있지만 이는 선택 사항이며 App 내부 구현입니다.

```ts
import { createApp, type WindforceContext } from "windforce-client"
import { createScrapingContext, runModule } from "@data-team/scraping-sdk"

export const main = createApp({
  actions: {
    scrape: async (coreCtx: WindforceContext) => {
      const scrapingCtx = createScrapingContext(coreCtx)
      return await runModule(scrapingCtx)
    },
  },
})
```

정확한 package와 function 이름은 App과 해당 SDK가 정합니다. 아키텍처에서 중요한 것은 의존 방향입니다.

```text
App -> 임의의 Application SDK -> Core Context 기능

Core -X-> SDK identity, SDK Context, Module 용어, Transport 구현
```

각 Application SDK는 작성자용 Context 형태, Method 의미, 호환성 matrix, Version, Fixture, Migration 안내, Core 기능 mapping을 소유합니다. Scraping SDK라면 4세대 `ScrapingContext`, `inputJson`, Logger 동작, `httpService.get/post/patch/put/delete/head/options`, Cookie, Encoding, Redirect, Proxy, Delay, Tracing, Playwright, Puppeteer 또는 Mobile bridge 지원 의미를 포함합니다.

`internalInputJson`, `InternalCall`, Tracer, AIB 같은 이전 표면마다 지원, 문서화된 Migration을 포함한 대체, 의도적인 미지원 중 하나를 명시하는 것도 SDK 책임입니다. Core는 이 SDK를 사용하는 App을 실행하기 위해 도메인 용어를 가질 필요가 없습니다.

## 작성 소스와 배포 Artifact 경계

Core의 Git Source 통신규격은 `windforce.json`, 선언된 Entrypoint, Schema 파일, 필요한 불투명 dependency가 포함된 정규 배포 Artifact에서 시작합니다. 작성 Repository가 코드를 정본으로 삼고 이 Artifact를 생성할 수도 있습니다. 이 경우 App 소유 Build Pipeline이 `bun main.ts --describe` 같은 SDK 전용 discovery를 실행하고, inline schema를 정규 파일로 기록하고, dependency를 bundle한 뒤 Core가 Register와 Sync할 배포 Git 또는 snapshot을 발행합니다.

Core는 `--describe`를 실행하거나 App을 import하거나 SDK에서 Manifest를 추론하지 않습니다. 그렇게 하면 등록 중 신뢰하지 않는 작성자 코드를 실행하고 Core를 SDK 전용 출력 형태에 결합하게 됩니다. Publish는 정규 Artifact에 대해 범용 정적 `main` export 검사와 dependency graph build만 수행합니다. 외부 `demo`, `sample` E2E 테스트는 생성된 배포 Git으로 이 경계를 증명한 뒤 로컬 및 원격 Worker에서 Register, Sync, Publish, Run, Result를 모두 검증합니다.

실제 App Repository가 있는 checkout에서는 다음과 같이 opt-in 외부 적합성 테스트를 실행합니다.

```powershell
$env:WINDFORCE_TYPESCRIPT_APPS_ROOT = 'C:\path\to\scraping\apps'
go test ./internal/server -run TestTypeScriptTier1ExternalAppsE2E -count=1 -v
```

## Core 책임

Core는 다음을 책임집니다.

- 정확한 Source revision을 동기화하고 불변의 준비된 Execution Bundle을 발행합니다.
- TypeScript를 Tier 1 Runtime으로 취급하되 명시적 Launcher 통신규격은 `typescript`, `python`, `go`만 허용합니다.
- App dependency를 보존하면서 해당 언어의 Core Author SDK를 주입하고 fingerprint를 계산합니다.
- Admission에서 Manifest, Action schema, Runtime, Entrypoint, `runsOn`, Timeout, Bundle digest를 검증하고 고정합니다.
- 적합한 Worker matching, Job claim과 lease, 고정 Bundle fetch와 검증, Bun, Python, Go 또는 Adapter command 선택을 수행합니다.
- Core Context를 만들고 Job 범위 Runtime access만 부여합니다.
- Backend 중립 Trace Context를 이어서 사용하거나 생성하고, Telemetry를 실행 필수조건으로 만들지 않으면서 읽기 전용 Carrier를 노출합니다.
- Cancel, Timeout, Drain, Log/Result masking, Completion, Retry 의미를 집행합니다.
- Run과 Invocation API를 통해 최종 Result를 반환합니다.

Core는 어떤 SDK package, SDK Context, Module envelope 또는 SDK 호환 버전도 import하거나 해석하지 않습니다. 최종 App Bundle이 Core App Runtime 통신규격을 만족하는지만 요구합니다.

## Runtime과 Worker capability 경계

Browser와 Mobile 실행에는 서로 다른 책임 영역이 있습니다.

- App과 dependency는 Playwright, Puppeteer, Mobile bridge와 App 수준 HTTP 동작의 사용 방법을 소유합니다.
- Core는 고정된 Launcher, `runsOn` Worker 요구조건, Label matching, Job lease, Bundle 전달, 실행 수명주기를 소유합니다.
- Self-hoster 또는 downstream fleet manager는 실제 Worker image, Browser binary, Mobile device, Capacity, Autoscaling을 소유합니다.

Core에서 capability와 label 용어는 이미 Worker matching에 사용됩니다. Application SDK는 Core의 `capabilities`, `runsOn`, Worker label 또는 WorkerPool을 자신의 Context negotiation 규격으로 다시 정의하면 안 됩니다. SDK 기능 탐색이 필요하다면 App 내부에서 관리하고 Core scheduling을 덮어쓰지 않아야 합니다.

## Version과 적합성 검증

독립적으로 변경되는 두 통신규격은 각각의 검증 근거가 필요합니다.

1. Core는 언어 wrapper, `WindforceContext`, Bundle preparation, Launcher, Job 범위 callback, Worker 수명주기를 테스트합니다.
2. 각 Application SDK는 지원하는 Core Author SDK/Runtime version에 대해 선택적인 Context adapter와 호환성 fixture를 테스트합니다.
3. Sample App은 수정하지 않은 Core가 조합된 Bundle을 발행하고 실행할 수 있음을 증명합니다.

Application SDK는 자신의 Context 통신규격에 `scraping.ctx/v1` 같은 이름을 발행할 수 있습니다. 이 식별자는 Core Manifest field가 아니며 Core가 SDK schema를 역직렬화할 이유가 되지 않습니다. 반대로 `WindforceContext`, private Launcher transport, `windforce.json`, Worker 실행 의미의 변경은 Core가 소유합니다.

## 변경 점검표

어느 쪽을 변경하든 다음을 확인합니다.

- App이 고정된 Entrypoint를 통해 계속 `main(coreCtx)`를 제공합니다.
- SDK 변환이 필요하다면 App process 안에서 이루어지고 App과 함께 Bundle에 포함됩니다.
- 어떤 Application SDK도 Core service credential이나 Worker Plane 권한을 받지 않습니다.
- Core가 SDK identity를 검사하거나 SDK 용어를 import하거나 SDK Context version을 해석하지 않습니다.
- `runsOn`과 Worker label이 SDK capability가 아니라 Core scheduling 입력으로 남습니다.
- Browser 또는 Mobile library 동작과 Worker provisioning 및 Job 수명주기가 분리됩니다.
- Core와 Application SDK 적합성 suite가 각자 소유한 경계를 검증합니다.

이 통신규격을 호출하는 Worker 절차는 [Worker 실행 수명주기](worker-execution.md)에, 외부 HTTP client 경계는 [Invocation API 영문 문서](../../concepts/public-api.md)에 설명되어 있습니다. 결정 이유는 [ADR 0021](../../adr/0021-keep-application-sdks-opaque-to-core.md)에 기록합니다.
