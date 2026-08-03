---
title: 실행 관측성과 디버깅
description: Core Worker의 Job 로그, 결과, 서비스 로그, 아티팩트, 디버거 책임 경계입니다.
---

이 문서는 Windforce Core에서 실행하는 App을 관측하고 디버깅하는 현재 통신규격을
사람과 AI Coding 에이전트가 이해할 수 있도록 정리한 정본입니다.

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

Worker 순서는 [Worker 실행 수명주기](worker-execution.md), 결정 이유와 Windmill
비교는 [ADR 0024](../../adr/0024-offset-job-log-streaming.md)를 참고합니다.
