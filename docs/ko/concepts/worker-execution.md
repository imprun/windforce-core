---
title: Worker 실행 수명주기
description: 고정된 Job이 실행 번들을 가져와 launcher를 시작하고 결과를 완료하는 정본 절차입니다.
---

이 문서는 Windforce Core Worker 실행 절차의 현재 정본입니다. Runtime 구현, 테스트, AI Coding 에이전트가 반드시 보존해야 하는 실행 순서와 책임 경계를 정의합니다.

> Trace 구현 상태 (2026-08-06): Trace 연속성 항목은 ADR 0029에서 승인하고 GitHub issue #128에서 추적하는 목표 통신규격입니다. 현재 Worker는 아직 W3C Trace Context를 저장, 복원 또는 주입하지 않습니다.

## 핵심 원칙

Worker는 Job에 고정된 불변 Execution Bundle을 실행합니다. Git, Source Store, 현재 Active Release 또는 JSON 파일을 직접 실행하지 않습니다.

`input.json`은 호출 입력값입니다. 애플리케이션 소스와 준비된 의존성은 별도의 Worker 로컬 Execution Bundle 캐시에 있습니다. Launcher는 이 Bundle을 가져와 검증한 다음에만 시작합니다.

## 서로 다른 두 수명주기

Release 발행과 Job 실행은 의도적으로 분리되어 있습니다.

| 단계 | 담당 | 해서는 안 되는 일 |
| --- | --- | --- |
| Sync | 정확한 Git commit을 가져오고 소스 메타데이터를 검증하여 불변 소스 snapshot을 Source Store에 materialize합니다. | Runtime 의존성을 설치하거나 Active Release를 선택하는 일 |
| Publish Release | 동기화된 snapshot을 가져와 의존성과 SDK를 준비하고 entrypoint를 검증한 뒤 완전한 tree를 digest로 발행하고 Release를 선택합니다. | Job을 만들거나 실행하는 일 |
| Run admission | Active Release를 한 번만 결정하고 완전한 Deployment와 선택적인 Versioned W3C 생성 Context를 Run과 Job에 고정합니다. | 소스를 가져오거나 애플리케이션 코드를 실행하는 일 |
| Worker 실행 | 고정된 Job을 claim하고 Polling의 현재 Context가 아니라 저장된 실행 Context를 선택하여 Attempt Span을 시작한 뒤 유효 입력을 구성하고 Execution Bundle을 가져와 검증하여 entrypoint를 실행하고 Job을 완료합니다. | Git을 읽거나 의존성을 준비하거나 Active Release를 다시 결정하거나 claim transport를 실행 parent로 사용하는 일 |

## 정본 실행 순서

다음 순서는 실행 통신규격의 일부입니다.

```text
Control Plane                                  Worker
-------------                                  ------
정확한 Git commit Sync
  -> Source Store snapshot

Publish Release
  -> 의존성 설치/빌드/SDK 주입
  -> entrypoint 검증
  -> sha256 Execution Bundle 발행
  -> 불변 Active Release

Run admission
  -> Deployment + Bundle digest 고정
  -> Trace Context 계속 사용 또는 생성
  -> Trace Context와 함께 Run + Job 생성
                                                Job + lease claim
                                                lease heartbeat 시작
                                                Poll Context가 아닌 저장된 Job Context 선택
                                                attempt 1: 생성 Trace 계속 사용
                                                attempt >1: Root + 생성 Link
                                                Context 없음: Worker Root 시작
                                                유효 입력 구성
                                                고정된 Execution Bundle 열기
                                                  -> 검증된 cache hit 또는
                                                  -> digest를 임시 경로에 fetch
                                                  -> 검증 후 원자적으로 승격
                                                  -> preparation fingerprint 검증
                                                  -> ready marker 기록
                                                Bundle 내부 entrypoint 결정
                                                Job 전용 임시 디렉터리 생성
                                                input.json + launcher wrapper 기록
                                                Bun/Python/Go/adapter command 시작
                                                마스킹된 로그 전송
                                                result.json 읽기
                                                Job 완료 또는 실패
```

구현에서 Processor는 `job.Payload.PinnedDeployment()`를 Runtime Runner에 전달합니다. 해당 실행 경로가 계측되면 Admission에서 고정한 선택적 생성 Context를 복원하고, Context가 없는 Legacy, 직접 생성 또는 테스트 Job이면 Worker Root를 시작합니다. `Runner.Run`은 정본 Executor가 Job 전용 디렉터리를 만들거나 `input.json`을 기록하기 전에 `openExecutionBundle`을 호출합니다. 그다음 Executor가 현재 W3C Carrier를 Private Transport로 주입하고 언어별 wrapper와 선택한 Runtime을 시작합니다. TypeScript에서는 `bun run wrapper.ts`가 가져온 Bundle 내부 entrypoint의 절대 경로를 import하여 `main(ctx)`를 호출합니다.

`scriptLang`은 정확히 `typescript`, `python`, `go` 중 하나로 정규화합니다. Manifest 호환성을 위해 생략하면 TypeScript가 되지만 다른 값은 준비 전에 거부하며 Bun으로 암묵적으로 fallback하지 않습니다. TypeScript 발행 시 Core는 Bun 정적 scanner로 이름 있는 `main` export를 요구한 뒤 `bun build`로 entrypoint dependency graph를 검증합니다. 두 단계 모두 App을 import하거나 실행하지 않으므로 발행 과정에서 App의 top-level side effect가 발생하지 않습니다.

## 파일시스템 분리

Worker는 서로 다른 두 위치를 사용합니다.

```text
<worker-cache>/execution-bundles/<digest>/
  main.ts
  node_modules/
  .ready
  .windforce-execution-ready
  ...준비된 애플리케이션 tree

<temporary-job-dir>/
  input.json
  wrapper.ts
  result.json
```

Bundle 캐시는 재사용 가능하며 고정된 digest로 식별합니다. Job 디렉터리는 일회성이며 실행별 입력, wrapper, 결과 파일만 가집니다. Wrapper가 Bundle 캐시의 entrypoint를 import하므로 애플리케이션 소스를 Job 디렉터리에 복사할 필요가 없습니다.

Core Launcher는 `WindforceContext`를 구성하고 App Entrypoint를 호출합니다. Core는 App이 어떤 SDK를 사용하는지 검사하지 않습니다. 모든 SDK는 App process 안의 불투명한 dependency이며 Bundle을 가져오거나 Launcher를 선택하거나 Job을 claim하거나 Worker Plane 권한을 받지 않습니다. 자세한 책임 경계는 [앱 런타임 통신규격과 SDK 책임 경계](app-runtime-interface.md)를 참고합니다.

## Bundle 획득과 캐시 안전성

Runtime은 모든 Job에서 비어 있지 않은 고정 Bundle digest를 요구합니다. `.windforce-execution-ready`에 해당 digest가 기록되어 있고 Bundle preparation fingerprint가 Worker Runtime과 호환될 때만 cache hit를 인정합니다.

Cache miss이면 같은 digest의 동시 요청을 하나로 합칩니다. Bundle을 같은 상위 경로의 임시 디렉터리에 가져오고 검증한 다음 digest 기반 캐시 디렉터리로 원자적으로 승격합니다. Runtime fingerprint를 승인한 뒤에만 ready marker를 기록합니다. 취소되었거나 없거나 손상되었거나 호환되지 않는 Bundle은 명명된 Bundle 오류로 실패해야 하며 Git, 의존성 설치, 컴파일 또는 다른 Release로 fallback해서는 안 됩니다.

## 로컬 Worker와 원격 Worker

로컬 Worker와 원격 Worker는 같은 순서와 고정 Bundle 의미를 보존합니다.

- 로컬 Worker는 설정된 Execution Artifact Store에서 digest를 읽어 Worker 로컬 캐시에 복사합니다.
- 원격 Worker는 `GET /worker/v1/artifacts/{digest}`를 요청합니다. Core는 설정된 Artifact Store를 읽어 tar archive로 전송하고, Worker는 임시 디렉터리에 압축을 풀어 POSIX 배포 환경에서 digest를 검증한 뒤 로컬 캐시로 원자적으로 승격합니다.
- 원격 Worker는 Core의 데이터베이스, Source Store 또는 Artifact Store 파일시스템을 mount하지 않습니다. Repository credential과 서버 암호화 root는 Core에 남습니다.

현재 서버 측 Artifact Store 구현은 파일시스템 기반입니다. 이것은 원격 Worker 전송 방식과 별개입니다. Core가 해당 파일시스템을 소유하고 Worker Plane을 통해 digest 기반 Artifact를 원격 Worker에 제공합니다.

## Worker 종료 수명주기

등록된 Worker 상태는 `active`입니다. Process가 interrupt 또는 termination signal을 받으면 새 Job claim을 중단하고 같은 registry record를 `draining`으로 갱신합니다. Registry와 Job lease heartbeat는 계속되며 이미 claim한 Job은 기본 30초인 `--drain-timeout` 동안 실행 context를 유지합니다. 그 안에 끝나면 정상적으로 최종 Result를 기록합니다. 제한시간이 지나면 Core가 Launcher를 취소하고 실패 Result를 기록하며, 종료 signal과 분리된 completion context로 lease-fenced 완료 처리까지 수행합니다.

실행 중 Job이 완료되거나 취소된 뒤 Worker는 등록을 해제합니다. `offline`은 남겨 둔 status row가 아니라 live registry에 record가 없는 상태입니다. Standalone과 독립 Worker command는 같은 interrupt 및 SIGTERM 처리를 설치하고, 로컬과 원격 backend는 같은 `active`, `draining` 상태를 전달합니다.

## 입력과 Secret 경계

Worker는 실행 전에 유효 입력을 구성합니다. Backend에 따라 in-process store가 구성하거나 Worker Plane이 prepared claim으로 반환할 수 있습니다. Runtime Variable, Resource, InputConfig 값과 Secret 참조가 해당 Job의 유효 입력이 되며, Secret 값은 로그와 결과 마스킹 대상으로 등록됩니다.

입력 구성은 Bundle 식별자를 변경하지 않습니다. 동일한 고정 Bundle이 서로 다른 Job 입력을 실행할 수 있습니다. `input.json`은 소스 배포 수단이 아니며 Repository credential이나 다른 애플리케이션 revision을 포함해서는 안 됩니다.

## 완료와 실패 의미

Launcher는 Action의 최종 값을 `result.json`에 기록합니다. Worker는 프로세스 실행 중 마스킹된 로그를 전송하고 결과를 읽어 Secret 값을 다시 마스킹한 뒤 lease가 걸린 Job을 성공, 실패 또는 사람 입력 대기 상태로 완료합니다. 프로세스 실패는 Job 결과로 표현하고, Bundle 획득 또는 Launcher 시작과 같은 harness 실패는 구조화된 Runtime 오류로 변환합니다.

Job이 대기 또는 실행 중일 때 새 Release를 발행해도 해당 Job은 바뀌지 않습니다. 상위 Workflow가 명시적으로 새 Run을 admit하지 않는 한 재시도도 기존 Run과 Job에 고정된 Deployment snapshot을 사용합니다.

App stdout과 stderr는 하나의 마스킹된 Job 로그 stream이며 Action의 최종 반환값은
별도 결과입니다. Offset 기반 실시간 추적, Service 로그, Browser Artifact, 공유
Worker에서 Bun Inspector를 노출하지 않는 원칙은
[실행 관측성과 디버깅](execution-observability.md)에 정의합니다.

## Maintainer와 AI Coding 에이전트 점검표

Worker 또는 Runtime 변경을 수용하기 전에 다음을 모두 확인해야 합니다.

- Job이 여전히 고정된 Deployment와 Bundle digest를 Runner에 제공합니다.
- Bundle 열기와 검증이 Job Runtime 파일 생성 및 프로세스 시작보다 먼저 일어납니다.
- 어떤 실행 경로도 Git clone, 의존성 설치, SDK 주입, 소스 컴파일 또는 Active Release 재조회를 하지 않습니다.
- Cache miss 처리가 임시 경로, 검증, 원자적 승격 및 동시 fetch 안전성을 보존합니다.
- Cache hit가 digest marker와 preparation fingerprint를 모두 검증합니다.
- Entrypoint containment가 가져온 Bundle root 밖으로 나가는 경로를 차단합니다.
- 로컬 및 원격 Worker 경로가 동일한 Bundle과 완료 의미를 보존합니다.
- 종료 시 새 claim을 중단하고 `active -> draining`을 노출하며 drain deadline까지 실행 중 Job을 보존한 뒤 완료 후에만 registry record를 제거합니다.
- 로그와 결과가 Secret 마스킹 및 lease fencing을 유지합니다.
- Trace Context가 없거나 잘못됐거나 너무 커도 실행을 막지 않습니다. Local, Remote, Standalone Worker는 claim transport의 현재 Context가 아니라 저장된 Job 생성 Context를 사용하고, 유효한 Job Context가 없으면 Worker Root를 시작하며, 유효 실행 Carrier만 Launcher에 전달합니다.
- Job은 영속 작업이고 Attempt는 lease로 fence된 한 번의 실행입니다. Attempt 1은 생성 Trace를 이어서 사용할 수 있고, `attempt > 1`의 lease 복구는 이전 attempt Context 저장을 요구하지 않으면서 생성 Context에 Link한 새 Root를 시작합니다. 멱등 replay는 Attempt를 만들거나 생성 Context를 바꾸지 않습니다.
- 로그 추가가 byte offset 기준으로 순서를 보존하고 재연결 가능하며 App 로그,
  최종 결과, Service 로그, Binary Artifact를 서로 섞지 않습니다.
- 테스트가 Bundle 발행/fetch, 캐시 동작, 원격 압축 해제, Runtime 실행, TypeScript `main` 정적 검증, 정상 drain과 timeout drain, Bundle 오류 시 Job 실패를 검증합니다.

주요 구현 위치는 `internal/worker`, `internal/runtime`, `internal/executor`, `internal/executionbundle`, `internal/remoteworker`, `internal/server/worker_plane.go`입니다. 실행 의미를 바꾸는 변경은 이 현재 상태 문서와 함께 ADR도 추가해야 합니다. 선택적인 Trace 전파와 독립 Root 생성은 [ADR 0029](../../adr/0029-optional-trace-context-continuity.md)에 정의합니다.
