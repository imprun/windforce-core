---
title: 실행 관측성과 디버깅
description: Core Worker의 Job 로그, 결과, 서비스 로그, 아티팩트, 디버거 책임 경계입니다.
---

이 문서는 Windforce Core에서 실행하는 App을 관측하고 디버깅하는 현재 통신규격을
사람과 AI Coding 에이전트가 이해할 수 있도록 정리한 정본입니다.

> Trace 구현 상태 (2026-08-06): 현재 Source는 GitHub issue #128에서 추적한 ADR 0029 통신규격을 구현합니다. W3C 생성 Context가 두 State Store, Worker Plane, attempt, Launcher transport와 Core Author SDK를 통과하며 Core Span을 OTLP로 내보낼 수 있습니다.

[English](../../concepts/execution-observability.md)

## Worker가 기록하는 것

Launcher process가 stdout과 stderr로 내보낸 텍스트를 Worker가 도착 순서대로
합치고, 잘못된 UTF-8을 치환하고, 구성된 Secret 값을 마스킹한 뒤 Job 로그에
추가합니다. Executor의 Job별 로그 제한이 전체 발생량을 제한합니다.

Action의 최종 반환값은 stdout에서 해석하지 않습니다. 언어별 wrapper가
`result.json`에 기록하고 Worker가 별도로 마스킹한 뒤 lease가 걸린 Job을
완료합니다. 로그와 결과는 서로 다른 통신규격입니다.

```text
Bun/Python/Go/adapter process
  stdout + stderr
    -> Worker Secret 마스킹
      -> append-only Job 로그 chunk
        -> 일반 텍스트 조회 또는 offset SSE

language wrapper
  result.json
    -> Worker 결과 마스킹
      -> 최종 Job 결과
```

## 분산 추적

독립적으로 호출할 수 있는 모든 경계에서 Trace Context는 선택 사항입니다. HTTP 또는 Protocol Ingress는 유효한 현재 Context를 이어서 사용하고, 없으면 유효한 W3C `traceparent`를 추출하며, 둘 다 없으면 해당 역할의 추적 SDK가 활성화됐을 때 새 Root를 시작합니다. 없거나 잘못됐거나 너무 큰 Trace Context 때문에 정상 요청이나 Job을 실패시키지 않으며 잘못된 원문은 Log에 남기지 않습니다.

Admission은 검증한 Versioned 생성 Context를 Application input과 분리하여 Run과 Job에 저장합니다. Local JSON 또는 PostgreSQL로 구성한 State Store가 Queue 대기 시간을 넘기는 영속 Carrier이며, 이를 위해 Admission Span을 Job 완료까지 열어 두지 않습니다. Local, Remote, Standalone Worker는 Polling이나 claim transport의 현재 Context가 아니라 저장된 Job 실행 Context를 사용합니다. Context가 없는 Legacy, 직접 생성 또는 테스트 Job은 Worker 추적이 활성화됐을 때 Worker Root를 시작합니다. Launcher는 Job 실행 Carrier만 Core private transport로 전달하고 Core Author SDK가 이를 읽기 전용으로 노출하므로, 불투명한 Application SDK가 계속 사용할 수 있습니다.

```text
계측된 Gateway 또는 Adapter (선택)
  -> Core API / Admission 또는 새 Core Root
    -> State Store Run + Job + 생성 Context
      -> attempt 1은 생성 Trace를 이어서 사용
      -> attempt >1은 생성 Context에 Link한 Root 시작
        -> Launcher와 Core Author SDK
          -> Application SDK / App / Action Span 또는 새 SDK Root
```

Remote Worker claim의 Client/Server Span은 Transport를 설명하며 Job Processing parent가 되지 않습니다. Attempt 1은 보통 생성 Trace를 이어서 사용합니다. Lease 복구로 같은 Job이 `attempt > 1`이 되면 불변 생성 Context에 Link한 새 Root를 시작합니다. Version 1은 이전 attempt Span Context를 저장하지 않습니다. Invocation 멱등 replay는 기존 Run과 Job을 반환하며 생성 Context를 바꾸거나 attempt를 만들지 않고, 호출자가 새 Run을 요청하면 새 생성 Context를 만듭니다. 현재 in-process HumanTask hold는 기존 attempt와 Trace를 유지하고, 미래 suspend/resume은 생성 Context에 Link한 새 attempt Trace를 시작합니다. 하나의 원인은 parent-child, 새 attempt 또는 여러 원인은 Link로 표현하며 Batch나 fan-out 자체만으로 Link를 강제하지 않습니다. `correlation_id`는 업무 상관관계 값이며 Trace ID가 아닙니다.

Core는 Backend 중립 OTLP를 내보내며 Tempo 같은 저장 Backend에 의존하지 않습니다. 표준 `OTEL_*` 설정이 SDK 활성화, Sampling, Export, Resource identity와 shutdown flush를 소유하며 Core는 별도 Sampling 신뢰 정책을 만들지 않습니다. 역할별 Export 활성화가 다른 배치에서도 Carrier 검증과 전달은 호환되며 Exporter 장애는 실행 상태를 바꾸지 않습니다. Service Log에는 외부 Log-to-Trace 이동을 위한 `trace_id`와 `span_id`를 넣을 수 있습니다. 고유한 Run, Job, attempt 식별자는 Metric label이나 Log index label에 넣지 않으며, 입력값, 결과, Credential, Token, Secret 값, 잘못된 원문 Carrier와 Baggage는 Trace attribute가 될 수 없습니다. 전체 결정과 적합성 기준은 [ADR 0029](../../adr/0029-optional-trace-context-continuity.md)에 기록합니다.

### Core OpenTelemetry 설정

Core는 해당 역할에서 Export하지 않아도 유효한 Carrier를 다음 경계로 전달할 수 있도록 기본적으로 Process 내부 Trace ID를 만듭니다. SDK 자체를 끄려면 `OTEL_SDK_DISABLED=true`를 지정합니다. 설정하지 않은 설치가 Local Collector에 반복 접속하지 않도록 `OTEL_TRACES_EXPORTER`, `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT` 중 하나를 명시했을 때만 Export합니다.

- `OTEL_TRACES_EXPORTER=otlp`, `console`, `none`으로 Export 동작을 선택합니다.
- `OTEL_EXPORTER_OTLP_TRACES_PROTOCOL` 또는 `OTEL_EXPORTER_OTLP_PROTOCOL`은 `http/protobuf`와 `grpc`를 지원합니다.
- 선택한 공식 Exporter가 표준 OTLP endpoint, header, timeout, compression, TLS 환경변수를 읽습니다.
- `OTEL_TRACES_SAMPLER`와 `OTEL_TRACES_SAMPLER_ARG`로 Parent-based, Always-on/off, Trace-ID-ratio Sampling을 정합니다.
- `OTEL_SERVICE_NAME`과 `OTEL_RESOURCE_ATTRIBUTES`로 Resource identity를 덮어쓸 수 있습니다. 기본 Service name은 `windforce-core-server`, `windforce-core-worker`, `windforce-core-standalone`이며 Standalone은 Resource 하나를 사용하고 Span attribute로 Component를 구분합니다.

Job 상태 응답은 운영자 이동을 위한 생성 `trace_id`만 노출하고 원문 `traceparent`와 `tracestate`는 노출하지 않습니다. 복구 attempt는 생성 Context에 Link한 새 Trace이므로 Version 1에서는 생성 Trace가 안정적인 조회 기준입니다.

## 로그 조회와 실시간 추적

보존된 로그를 내려받을 때는 일반 endpoint를 사용합니다.

```http
GET /api/w/{workspace}/jobs/{jobId}/logs?tail_bytes=65536
```

실행 중인 Job을 추적할 때는 SSE endpoint를 사용합니다.

```http
GET /api/w/{workspace}/jobs/{jobId}/logs/stream?offset=0&timeout_seconds=60
Accept: text/event-stream
```

```json
{
  "type": "update",
  "running": true,
  "completed": false,
  "new_logs": "processing page 3\n",
  "log_offset": 137,
  "status": "running",
  "attempt": 1,
  "worker_id": "worker-browser-2"
}
```

`new_logs`를 소비한 뒤 `log_offset`을 보관합니다. `timeout`이면 해당 offset으로
다시 연결합니다. `ping`에는 로그가 없습니다. 마지막 update는
`completed: true`이고 서버가 연결을 닫습니다. Offset은 글자 수가 아니라 UTF-8
byte 수입니다. 인증과 Workspace 권한 검증은 다른 Control Plane Job endpoint와
같습니다.

저장소 로컬 개발 보조 도구는 해당 재연결 loop를 제공합니다. 이것은 별도로
배포되는 Core 사용자용 CLI가 아닙니다.

```bash
python tools/windforce_control.py --workspace default job-logs \
  --job-id <job-id> --follow
```

## 반드시 분리할 다섯 표면

1. **App Job 로그**: 하나의 Workspace 범위 Job에서 발생한 마스킹된
   stdout/stderr입니다. 진행상황, 진단, stack trace에 사용합니다.
2. **Job 결과**: Action의 최종 반환값 또는 구조화된 실패입니다. Result endpoint나
   Invocation 완료 통신규격으로 읽습니다.
3. **Worker/Core 서비스 로그**: Process health, claim, lease, transport,
   infrastructure 정보를 다룹니다. Host, container, cluster logging stack에서
   수집하며 각 Job 로그에 복사하지 않습니다.
4. **Job 아티팩트**: Screenshot, Playwright trace, video, HAR, crash dump 같은
   binary 증거입니다. Core에는 아직 Artifact API가 없으므로 stdout이나 결과에
   base64로 넣지 않습니다.
5. **대화형 소스 디버깅**: 공유 Worker에서는 지원하지 않습니다. 일반 Job에서
   Bun/Node Inspector를 노출하지 않습니다.

## TypeScript와 브라우저 App 디버깅 절차

Bun/TypeScript App은 간결한 구조화 진행상황을 stdout에, stack을 포함한 오류를
stderr에 기록합니다. 콘솔에서 Job ID로 로그 검사기를 열어 완료될 때까지
추적하고, Job의 Release commit과 Bundle digest를 함께 확인합니다. Breakpoint나
브라우저 devtools가 필요하면 안전한 입력으로 동일한 `main(ctx)`를 로컬에서
재현합니다.

Playwright나 Puppeteer는 불변 Execution Bundle 안의 App dependency입니다.
Core가 해당 SDK를 알아야 process 로그를 수집할 수 있는 것은 아닙니다. Artifact
통신규격이 생기기 전에는 App이 명시적으로 설정한 외부 저장소에 browser trace를
보관하고 안전한 참조만 로그로 남깁니다. Credential이나 binary 본문을 로그에
넣지 않습니다.

## 개발과 진단 절차

Core는 다음 세 가지 절차를 의도적으로 분리합니다.

1. **App 단위 개발**: TypeScript 프로젝트를 Bun으로 직접 실행하고 App SDK의
   test context 또는 mock을 사용합니다. 순수 App logic test, breakpoint,
   browser devtools에는 Core를 실행할 필요가 없습니다.
2. **Core 통합 개발**: `windforce-core standalone`을 실행하고 불변 Execution
   Bundle을 게시한 뒤 Public API로 App을 호출합니다. 생성된 Job은 Web Console
   또는 Control Plane API로 확인합니다.
3. **Worker 장애 진단**: 정확한 Release commit, Bundle digest, 마스킹된 Job 로그,
   최종 결과, Worker 서비스 로그를 함께 봅니다. 개발 중 `--tee-job-logs` 옵션으로
   캡처한 chunk를 로컬 Worker process 로그에도 보낼 수 있지만, Job 로그 저장이나
   권한 검증을 대신하지 않습니다.

`windforce-core`는 Runtime 실행 파일입니다. `tools/windforce_control.py`는 두 번째
절차를 돕는 저장소 로컬 도구일 뿐, 지원되는 사용자용 CLI나 App Authoring
통신규격이 아닙니다.

Windmill도 같은 경계를 사용합니다. 별도 배포되는 `wmill` CLI가 Workspace 동기화,
metadata 생성, preview, Job 조회를 담당하고 Bun script는 로컬 context를 주입하여
직접 실행할 수도 있습니다. 운영 Worker는 별도로 child process를 실행하고
stdout/stderr를 캡처하여 증분 저장하며 최종 결과와 다른 경로로 update를
제공합니다. Core가 참고하는 것은 이 책임 분리이며 Windmill Workspace 파일 형식이나
CLI를 Runtime dependency로 채택하는 것이 아닙니다. 자세한 내용은
[Windmill 로컬 개발](https://www.windmill.dev/docs/advanced/local_development),
[로컬 Script 실행](https://www.windmill.dev/docs/advanced/local_development/run_locally),
[Worker와 Worker Group](https://www.windmill.dev/docs/core_concepts/worker_groups)을
참고합니다.

## Maintainer와 AI Coding 에이전트 규칙

- 새 로그 경로가 Worker의 Secret 마스킹을 우회하면 안 됩니다.
- 디버깅에 모두 유용하다는 이유로 stdout, Job 결과, Worker 서비스 로그,
  아티팩트를 하나의 schema로 합치지 않습니다.
- 모든 조회와 stream에 `Workspace + Job ID` 권한 경계를 유지합니다.
- `log_offset`은 byte cursor이며 재연결은 idempotent해야 합니다.
- Producer capture, 저장 보존, event 크기, client memory를 각각 제한합니다.
- Retention이 Job을 지울 때 로그 chunk와 cursor도 함께 삭제되어야 합니다.
- 일반 Executor에 `bun --inspect`를 추가하지 않습니다. 격리 Debug Session은 별도
  ADR, 인증 tunnel, TTL, audit, Secret 정책이 필요합니다.

Worker 순서는 [Worker 실행 수명주기](worker-execution.md), Log Streaming 결정 이유와 Windmill 비교는 [ADR 0024](../../adr/0024-offset-job-log-streaming.md), 분산 Trace 연속성은 [ADR 0029](../../adr/0029-optional-trace-context-continuity.md)를 참고합니다.
