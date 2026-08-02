---
title: 워크스페이스
description: 하나의 Windforce Core 인스턴스 안에서 관리되는 상태와 권한 경계입니다.
---

워크스페이스는 하나의 Windforce Core 인스턴스 안에서 앱, 릴리스, 클라이언트, 입력 설정, Job, 인바운드 트리거, 아웃바운드 Webhook, 변수, 리소스와 감사 기록을 묶는 경계입니다. 각 워크스페이스는 변경할 수 없는 ID, 표시 이름, 수명 주기 상태와 0개 이상의 이름 있는 범위 제한 자격 증명을 가집니다.

## 정보

워크스페이스 ID는 2~48자의 영문 소문자, 숫자와 하이픈으로 이루어진 slug이며 영문 소문자로 시작하고 영문 소문자 또는 숫자로 끝납니다. API 경로와 저장된 리소스 key에 포함되므로 바꿀 수 없습니다. 표시 이름은 변경할 수 있습니다.

`default`는 항상 존재하며 보관하거나 삭제할 수 없습니다. 로컬 개발과 단일 워크스페이스 설치의 초기 워크스페이스입니다.

## 접근

Windforce Core는 다음 API principal을 구분합니다.

| Principal | 범위 | 자격 증명 |
| --- | --- | --- |
| Instance administrator | 전역 workspace 수명 주기와 모든 workspace | `--admin-token-env`로 지정한 token |
| Workspace principal | 한 workspace의 전체 운영자 권한 | 명시적으로 발급하거나 회전할 때 한 번 반환하는 이름 있는 `wfw_` token |
| Client principal | 한 workspace 안의 한 client 호출과 client별 input 설정 | client 생성 또는 회전 시 한 번 반환하는 `wfk_` token |
| Service principal | 한 workspace의 system-to-system 호출, 선택적으로 app/action 제한 | `wfs_` token |
| Job principal | 한 job과 workspace의 SDK callback endpoint | 수명이 짧은 job token |

워크스페이스 생성과 자격 증명 발급은 별도 작업입니다. Workspace token은 SHA-256 hash로 저장하고 안정적인 ID와 운영자가 정한 이름을 가집니다. 원문 token은 발급 또는 회전할 때만 한 번 표시합니다. Workspace principal은 다른 workspace나 전역 workspace/credential 수명 주기를 제어할 수 없습니다.

Core가 소유하는 bearer는 `wf` family prefix를 사용합니다. Job token은 `wfjob_`, workspace token은 `wfw_`, client token은 `wfk_`, service principal token은 `wfs_`, remote worker plane token은 `wfr_`입니다. 앞단 platform이나 proxy는 이 token을 Cloud token으로 교환하지 않고 그대로 Core에 전달해야 합니다.

공유 환경에서는 instance-admin token을 반드시 설정해야 합니다. 설정하지 않은 로컬 개발 환경은 인증 없이 요청을 허용할 수 있습니다.

## 수명 주기

활성 워크스페이스는 control-plane 변경과 새 실행 요청을 받습니다. 보관하면 상태와 감사 기록을 유지하면서 설정 변경, 자격 증명 발급·회전, 릴리스, trigger/Webhook 변경과 새 Run을 차단합니다. 읽기, 감사 조회, provisioning export와 credential 폐기는 계속 사용할 수 있습니다.

Instance administrator는 `DELETE /api/workspaces/{workspace}`로 워크스페이스를 영구 삭제할 수 있습니다. Registry와 함께 Run, Job, 앱 릴리스, trigger, route binding, Webhook, 변수, 리소스, input config, credential, 암호화 key 및 audit를 하나의 저장소 transaction에서 모두 제거합니다. 이 작업은 되돌릴 수 없으며 `default`는 삭제할 수 없습니다. 보관된 워크스페이스의 재활성화는 제공하지 않습니다.

## 운영 화면

상단 워크스페이스 전환기로 현재 워크스페이스를 바꿉니다. **워크스페이스 관리**는 생성과 전환만 담당합니다. 활성 워크스페이스의 **설정 → 워크스페이스**에서 표시 이름, 접근 자격 증명과 수명 주기를 관리합니다. 영구 삭제 버튼을 활성화하려면 워크스페이스 표시 이름을 정확히 입력해야 하며, 삭제에 성공하면 브라우저가 `default`로 전환됩니다. 감사 화면은 identity, access와 lifecycle 기록을 `workspace` 범주로 통합합니다.

전역 수명 주기 API는 `/api/workspaces`, workspace resource는 `/api/w/{workspace}`, 모든 Run 호출은 `/api/v1/workspaces/{workspace}`를 정본 경로로 사용합니다.
