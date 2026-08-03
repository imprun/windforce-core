---
title: HumanTask hold
description: 사람의 결정을 기다리는 동안 실행 중인 Action과 브라우저 세션을 유지합니다.
---

`HumanTask` hold는 Windforce Core의 범용 사람 결정 기능입니다. 원래 Action 프로세스를 종료하지 않고 같은 `await`에 결정값을 반환합니다. 워크플로 체크포인트도, Action 재실행도 아닙니다.

## 앱 개발 흐름

Phase 1은 TypeScript Author API를 제공합니다.

```ts
export async function main(ctx) {
  const page = await openAuthenticatedPage()

  const decision = await ctx.human.wait<{ otp: string }>({
    key: "login-otp",
    kind: "form",
    title: "인증번호 입력",
    inputSchema: {
      type: "object",
      required: ["otp"],
      properties: {
        otp: { type: "string", title: "인증번호" },
      },
    },
    privateContext: { accountRef: "opaque-reference" },
    timeoutMs: 120_000,
  })

  if (decision.outcome === "cancel") throw new Error("operator canceled")
  await page.getByLabel("인증번호").fill(decision.value.otp)
  return await continueScraping(page)
}
```

`key`는 한 Job attempt 안의 논리적 대기 지점을 안정적으로 식별합니다. 생략하면 wrapper가 한 번 생성하지만, 진단을 위해 명시하는 편이 좋습니다. 일시적인 서버·네트워크 장애는 같은 key로 재연결합니다. 같은 key에 다른 요청을 보내면 충돌로 거절합니다.

## 요청과 결정

요청에는 범용 `form`, 제목, 설명, JSON Schema, 선택적 표시 힌트, 비공개 컨텍스트와 제한 시간이 들어갑니다. Core는 제출값을 JSON Schema로 검증합니다. 내장 콘솔은 일반적인 object 필드(`string`, 문자열 enum, `number`, `integer`, `boolean`)를 표시하며 비공개 컨텍스트는 절대 보여주지 않습니다.

결과에는 `taskId`, `outcome`(`submit` 또는 `cancel`)과 선택적 `value`가 있습니다. HumanTask 취소 결정과 Run 취소, Action timeout, 작업 deadline, worker shutdown, lease loss는 서로 다른 종료 원인입니다.

## 요청·응답 흐름

```mermaid
flowchart TB
  A[Action에서 ctx.human.wait 호출] --> B[Job token으로 runtime API 호출]
  B --> C[대기 HumanTask 영속화]
  C --> D[Worker lease와 Bun 프로세스 유지]
  D --> E[콘솔 또는 외부 연동이 메타데이터 조회]
  E --> F[인증된 idempotent 결정]
  F --> G[결정값 암호화 저장과 감사 이벤트]
  G --> H[동일 Bun await에 결정 반환]
  H --> I[동일 브라우저 세션으로 Action 계속]
```

| 작업 | 엔드포인트 | 권한 |
| --- | --- | --- |
| Action 대기 | `POST /api/w/{workspace}/human-tasks/wait` | 현재 Job token 전용 |
| 목록 | `GET /api/w/{workspace}/human-tasks?state=pending` | 워크스페이스 운영자 또는 `human_tasks:read` |
| 상세 | `GET /api/w/{workspace}/human-tasks/{id}` | 워크스페이스 운영자 또는 `human_tasks:read` |
| 결정 | `POST /api/w/{workspace}/human-tasks/{id}/decision` | 워크스페이스 운영자 또는 `human_tasks:decide` |

결정에는 항상 `Idempotency-Key`가 필요합니다. service principal의 `allowed_targets`는 목록과 개별 작업 모두에서 App/Action 범위를 제한합니다.

## 암호화와 생명 주기

Core는 Action을 기다리게 하기 전에 작업을 영속화합니다. 메타데이터와 JSON Schema는 권한 있는 운영자가 읽을 수 있습니다. `privateContext`와 결정값은 Core의 암호화 root로 암호화되며 API 목록·상세, 로그, 감사 payload에는 나오지 않습니다. 복호화한 결정은 해당 Job의 대기 호출에만 반환합니다.

hold 동안 worker slot, Job lease, Bun 프로세스, 호출 스택, 앱 SDK 객체, Playwright/Puppeteer 세션이 모두 살아 있습니다. 따라서 유한한 timeout을 사용해야 하며 며칠 동안의 장기 대기에 쓰면 안 됩니다. suspend/checkpoint/re-entry는 재생과 앱 상태 규칙이 필요한 별도 후속 단계입니다.

Core는 회사의 Interaction 양식, `ACTION`, `U0001`, RMQ, scraping SDK 문법을 알지 않습니다. 앱 SDK는 `ctx.human.wait`를 감싸 회사 전용 표현과 외부 알림 채널을 소유할 수 있습니다. [앱 런타임 인터페이스](app-runtime-interface.md)와 [ADR 0026](../../adr/0026-human-task-hold.md)을 함께 보십시오.
