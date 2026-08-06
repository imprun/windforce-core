package executor

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRuntimeForGoUsesPreparedBinary(t *testing.T) {
	rt, err := runtimeFor("go")
	if err != nil {
		t.Fatalf("runtimeFor(go): %v", err)
	}
	if rt.label != "go" || rt.wrapperName != "" {
		t.Fatalf("runtimeFor(go) = {label:%q wrapper:%q}, want go/empty", rt.label, rt.wrapperName)
	}
	if rt.wrapperContent != nil {
		t.Fatalf("runtimeFor(go).wrapperContent is not nil")
	}
	bin := filepath.Join(t.TempDir(), "app")
	argv := rt.argv(RunParams{EntrypointAbsPath: bin})
	if len(argv) != 1 || argv[0] != bin {
		t.Fatalf("runtimeFor(go).argv = %#v, want [%s]", argv, bin)
	}
}

func TestRunPythonBuildsCanonicalCtxHelpers(t *testing.T) {
	requirePython(t)
	var stateSetBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/custom" && r.Header.Get("Authorization") != "Bearer job-token" {
			t.Errorf("authorization header = %q", r.Header.Get("Authorization"))
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/w/ws-a/variables/get/p/secret" && r.URL.RawQuery == "":
			if r.Header.Get("X-Windforce-Job-ID") != "" {
				t.Errorf("unexpected job id header = %q", r.Header.Get("X-Windforce-Job-ID"))
			}
			writeJSON(w, map[string]string{"value": "var-ok"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/w/ws-a/resources/get/p/browser":
			writeJSON(w, map[string]string{"resource": "browser-ok"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/w/ws-a/state" && r.URL.Query().Get("path") == "demo/echo":
			writeJSON(w, map[string]string{"state": "before"})
		case r.Method == http.MethodPost && r.URL.Path == "/api/w/ws-a/state" && r.URL.Query().Get("path") == "demo/echo":
			if err := json.NewDecoder(r.Body).Decode(&stateSetBody); err != nil {
				t.Errorf("decode state body: %v", err)
			}
			writeJSON(w, map[string]bool{"ok": true})
		case r.Method == http.MethodPost && r.URL.Path == "/custom":
			writeJSON(w, map[string]string{"custom": r.Header.Get("Authorization")})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	entrypoint := filepath.Join(t.TempDir(), "main.py")
	if err := os.WriteFile(entrypoint, []byte(`
async def main(ctx):
    ctx.logger.info("stdout-line", ctx.app, ctx.action)
    variable = await ctx.variables.get("secret")
    resource = await ctx.resources.get("browser")
    before = await ctx.state.get()
    await ctx.state.set({"message": ctx.input["message"]})
    custom = await (await ctx.http.fetch("/custom", method="POST", body={"x": 1})).json()
    return {
        "variable": variable,
        "resource": resource,
        "before": before,
        "custom": custom,
        "has_approval": hasattr(ctx, "approval"),
        "has_flow": hasattr(ctx, "flow"),
        "flow_resume_value": ctx.flow.resume_value,
        "headers": ctx.trigger.headers,
        "job": {"id": ctx.job.id, "workspace": ctx.job.workspace, "tag": ctx.job.tag},
    }
`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), RunParams{
		ScriptLang:        "python",
		BaseDir:           t.TempDir(),
		EntrypointAbsPath: entrypoint,
		Input:             []byte(`{"message":"hello"}`),
		Env: []string{
			"WF_JOB_ID=job-a",
			"WF_WORKSPACE=ws-a",
			"WF_BASE_URL=" + server.URL,
			"WF_TOKEN=job-token",
			"WF_APP=demo",
			"WF_ACTION=echo",
			"WF_TAG=default",
			"WF_STATE_PATH=demo/echo",
			"WF_TRIGGER_KIND=flow_resume",
			`WF_TRIGGER_HEADERS={"X-Test":"ok"}`,
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !res.Success() {
		t.Fatalf("success = false, exit=%d, result=%s, logs=%s", res.ExitCode, res.Result, res.Logs)
	}
	if !strings.Contains(res.Logs, "stdout-line demo echo") {
		t.Fatalf("logs = %q", res.Logs)
	}
	if stateSetBody["message"] != "hello" {
		t.Fatalf("state set body = %#v", stateSetBody)
	}
	var output struct {
		Variable    string            `json:"variable"`
		Resource    map[string]string `json:"resource"`
		Before      map[string]string `json:"before"`
		Custom      map[string]string `json:"custom"`
		HasApproval bool              `json:"has_approval"`
		HasFlow     bool              `json:"has_flow"`
		FlowResume  map[string]string `json:"flow_resume_value"`
		Headers     map[string]string `json:"headers"`
		Job         map[string]string `json:"job"`
	}
	if err := json.Unmarshal(res.Result, &output); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if output.Variable != "var-ok" || output.Resource["resource"] != "browser-ok" ||
		output.Before["state"] != "before" || output.Custom["custom"] != "Bearer job-token" ||
		!output.HasApproval || !output.HasFlow || output.FlowResume["message"] != "hello" ||
		output.Headers["X-Test"] != "ok" || output.Job["id"] != "job-a" ||
		output.Job["workspace"] != "ws-a" || output.Job["tag"] != "default" {
		t.Fatalf("output = %#v", output)
	}
}

func TestRunTypeScriptHumanTaskHoldPreservesProcessAndMemory(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun is not installed")
	}
	requestReceived := make(chan map[string]any, 1)
	releaseDecision := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/w/ws-a/human-tasks/wait" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer job-token" {
			t.Errorf("authorization header = %q", r.Header.Get("Authorization"))
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode HumanTask request: %v", err)
		}
		requestReceived <- body
		<-releaseDecision
		writeJSON(w, map[string]any{
			"task_id": "task-a",
			"outcome": "submit",
			"value":   map[string]string{"otp": "123456"},
		})
	}))
	defer server.Close()

	entrypoint := filepath.Join(t.TempDir(), "main.ts")
	if err := os.WriteFile(entrypoint, []byte(`
export async function main(ctx) {
  const pidBefore = process.pid
  const browserSession = { cookie: "session-cookie", marker: Symbol("browser-context") }
  const sameReference = browserSession
  const decision = await ctx.human.wait({
    key: "login-otp",
    kind: "form",
    title: "Enter code",
    inputSchema: {
      type: "object",
      required: ["otp"],
      properties: { otp: { type: "string" } },
    },
    privateContext: { callback: "opaque" },
    timeoutMs: 30000,
  })
  return {
    pidBefore,
    pidAfter: process.pid,
    sameReference: sameReference === browserSession,
    cookie: browserSession.cookie,
    decision,
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}
	type runResult struct {
		result Result
		err    error
	}
	runDone := make(chan runResult, 1)
	go func() {
		result, err := Run(context.Background(), RunParams{
			BunPath:           bun,
			ScriptLang:        "typescript",
			BaseDir:           t.TempDir(),
			EntrypointAbsPath: entrypoint,
			Input:             []byte(`{}`),
			Env: append(os.Environ(),
				"WF_JOB_ID=job-a",
				"WF_WORKSPACE=ws-a",
				"WF_BASE_URL="+server.URL,
				"WF_TOKEN=job-token",
				"WF_APP=demo",
				"WF_ACTION=wait",
			),
			Timeout: time.Minute,
		})
		runDone <- runResult{result: result, err: err}
	}()
	select {
	case request := <-requestReceived:
		if request["key"] != "login-otp" || request["title"] != "Enter code" || request["private_context"] == nil {
			t.Fatalf("HumanTask request = %#v", request)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("TypeScript Action did not enter HumanTask hold")
	}
	close(releaseDecision)
	select {
	case finished := <-runDone:
		if finished.err != nil || !finished.result.Success() {
			t.Fatalf("Run = result:%#v err:%v", finished.result, finished.err)
		}
		var output struct {
			PIDBefore     int  `json:"pidBefore"`
			PIDAfter      int  `json:"pidAfter"`
			SameReference bool `json:"sameReference"`
			Cookie        string
			Decision      struct {
				TaskID  string            `json:"taskId"`
				Outcome string            `json:"outcome"`
				Value   map[string]string `json:"value"`
			} `json:"decision"`
		}
		if err := json.Unmarshal(finished.result.Result, &output); err != nil {
			t.Fatalf("decode result %s: %v", finished.result.Result, err)
		}
		if output.PIDBefore == 0 || output.PIDBefore != output.PIDAfter || !output.SameReference || output.Cookie != "session-cookie" ||
			output.Decision.TaskID != "task-a" || output.Decision.Value["otp"] != "123456" {
			t.Fatalf("hold did not preserve process state: %#v", output)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("TypeScript Action did not resume after HumanTask decision")
	}
}

func TestRunTypeScriptHumanTaskHoldPreservesLiveChromiumSession(t *testing.T) {
	bun, err := exec.LookPath("bun")
	if err != nil {
		t.Skip("bun is not installed")
	}
	chrome := chromiumExecutable()
	if chrome == "" {
		t.Skip("Chrome or Chromium is not installed")
	}
	requestReceived := make(chan struct{}, 1)
	releaseDecision := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/fixture":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<!doctype html><title>HumanTask hold fixture</title><main id="app">ready</main>`))
		case "/api/w/ws-a/human-tasks/wait":
			requestReceived <- struct{}{}
			<-releaseDecision
			writeJSON(w, map[string]any{"task_id": "task-browser", "outcome": "submit", "value": map[string]string{"otp": "654321"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	entrypoint := filepath.Join(t.TempDir(), "browser-hold.ts")
	if err := os.WriteFile(entrypoint, []byte(`
async function devtoolsURL(process) {
  const reader = process.stderr.getReader()
  const decoder = new TextDecoder()
  let text = ""
  while (true) {
    const { value, done } = await reader.read()
    if (done) throw new Error("Chromium exited before exposing DevTools")
    text += decoder.decode(value, { stream: true })
    const match = text.match(/DevTools listening on (ws:\/\/[^\s]+)/)
    if (match) return match[1]
  }
}

function cdp(url) {
  const socket = new WebSocket(url)
  let nextID = 1
  const pending = new Map()
  const ready = new Promise((resolve, reject) => {
    socket.onopen = resolve
    socket.onerror = reject
  })
  socket.onmessage = (event) => {
    const message = JSON.parse(String(event.data))
    if (!message.id) return
    const item = pending.get(message.id)
    pending.delete(message.id)
    if (message.error) item.reject(new Error(message.error.message))
    else item.resolve(message.result)
  }
  return {
    async send(method, params = {}) {
      await ready
      const id = nextID++
      const response = new Promise((resolve, reject) => pending.set(id, { resolve, reject }))
      socket.send(JSON.stringify({ id, method, params }))
      return response
    },
    close() { socket.close() },
  }
}

async function waitForFixture(session) {
  const deadline = Date.now() + 15000
  while (Date.now() < deadline) {
    const result = await session.send("Runtime.evaluate", {
      expression: 'document.readyState === "complete" && document.title === "HumanTask hold fixture" && document.querySelector("#app")?.textContent === "ready"',
      returnByValue: true,
    })
    if (result.result.value === true) return
    await new Promise((resolve) => setTimeout(resolve, 50))
  }
  throw new Error("Chromium fixture did not finish loading")
}

export async function main(ctx) {
  const profile = (process.env.TEMP || ".") + "/wf-human-" + crypto.randomUUID()
  const browser = Bun.spawn([
    process.env.WF_TEST_CHROME,
    "--headless=new",
    "--remote-debugging-port=0",
    "--user-data-dir=" + profile,
    "--no-first-run",
    "--disable-default-apps",
    "about:blank",
  ], { stdout: "ignore", stderr: "pipe" })
  let session
  try {
    const browserWS = await devtoolsURL(browser)
    const debugOrigin = new URL(browserWS.replace("ws://", "http://")).origin
    const target = await fetch(debugOrigin + "/json/new?" + encodeURIComponent(process.env.WF_TEST_PAGE), { method: "PUT" }).then((response) => response.json())
    session = cdp(target.webSocketDebuggerUrl)
    await session.send("Runtime.enable")
    await waitForFixture(session)
    const before = await session.send("Runtime.evaluate", {
      expression: 'window.__wfSession = { marker: "browser-session" }; window.__wfAlias = window.__wfSession; localStorage.setItem("wf-cookie", "preserved"); ({ title: document.title, marker: window.__wfSession.marker })',
      returnByValue: true,
    })
    const browserPIDBefore = browser.pid
    const decision = await ctx.human.wait({
      key: "browser-otp",
      title: "Enter browser code",
      inputSchema: { type: "object", required: ["otp"], properties: { otp: { type: "string" } } },
      timeoutMs: 30000,
    })
    const after = await session.send("Runtime.evaluate", {
      expression: '({ sameReference: window.__wfAlias === window.__wfSession, marker: window.__wfSession.marker, stored: localStorage.getItem("wf-cookie"), title: document.title })',
      returnByValue: true,
    })
    return {
      browserPIDBefore,
      browserPIDAfter: browser.pid,
      before: before.result.value,
      after: after.result.value,
      decision,
    }
  } finally {
    session?.close()
    browser.kill()
    await browser.exited
  }
}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	type runResult struct {
		result Result
		err    error
	}
	runDone := make(chan runResult, 1)
	go func() {
		result, err := Run(context.Background(), RunParams{
			BunPath: bun, ScriptLang: "typescript", BaseDir: t.TempDir(), EntrypointAbsPath: entrypoint,
			Input: []byte(`{}`), Timeout: time.Minute,
			Env: append(os.Environ(),
				"WF_JOB_ID=job-browser", "WF_WORKSPACE=ws-a", "WF_BASE_URL="+server.URL,
				"WF_TOKEN=job-token", "WF_APP=browser-demo", "WF_ACTION=wait",
				"WF_TEST_CHROME="+chrome, "WF_TEST_PAGE="+server.URL+"/fixture",
			),
		})
		runDone <- runResult{result: result, err: err}
	}()
	select {
	case <-requestReceived:
	case <-time.After(15 * time.Second):
		t.Fatal("Chromium Action did not enter HumanTask hold")
	}
	close(releaseDecision)
	select {
	case finished := <-runDone:
		if finished.err != nil || !finished.result.Success() {
			t.Fatalf("Chromium Action failed: result=%s logs=%s err=%v", finished.result.Result, finished.result.Logs, finished.err)
		}
		var output struct {
			BrowserPIDBefore int `json:"browserPIDBefore"`
			BrowserPIDAfter  int `json:"browserPIDAfter"`
			Before           struct{ Title, Marker string }
			After            struct {
				Title, Marker, Stored string
				SameReference         bool `json:"sameReference"`
			}
			Decision struct{ Value map[string]string }
		}
		if err := json.Unmarshal(finished.result.Result, &output); err != nil {
			t.Fatalf("decode Chromium result %s: %v", finished.result.Result, err)
		}
		if output.BrowserPIDBefore == 0 || output.BrowserPIDBefore != output.BrowserPIDAfter ||
			output.Before.Title != "HumanTask hold fixture" || output.After.Title != output.Before.Title ||
			!output.After.SameReference || output.After.Marker != "browser-session" || output.After.Stored != "preserved" ||
			output.Decision.Value["otp"] != "654321" {
			t.Fatalf("hold did not preserve live Chromium state: %#v", output)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Chromium Action did not resume after HumanTask decision")
	}
}

func chromiumExecutable() string {
	for _, candidate := range []string{
		os.Getenv("WF_TEST_CHROME"),
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
	} {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	for _, name := range []string{"google-chrome", "chromium", "chromium-browser", "msedge"} {
		if candidate, err := exec.LookPath(name); err == nil {
			return candidate
		}
	}
	return ""
}

func TestRunPythonConfiguresLoggingFromEnv(t *testing.T) {
	requirePython(t)
	entrypoint := filepath.Join(t.TempDir(), "main.py")
	if err := os.WriteFile(entrypoint, []byte(`
import logging


async def main(ctx):
    logging.getLogger("scraping.sdk").debug("debug sdk log %s", ctx.action)
    logging.getLogger("gov24.plus").info("info fcode log %s", ctx.app)
    return {"ok": True}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), RunParams{
		ScriptLang:        "python",
		BaseDir:           t.TempDir(),
		EntrypointAbsPath: entrypoint,
		Input:             []byte(`{}`),
		Env: []string{
			"WF_WORKSPACE=ws-a",
			"WF_APP=MLMWGM",
			"WF_ACTION=50",
			"LOG_LEVEL=DEBUG",
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !res.Success() {
		t.Fatalf("success = false, exit=%d, result=%s, logs=%s", res.ExitCode, res.Result, res.Logs)
	}
	if !strings.Contains(res.Logs, "DEBUG scraping.sdk debug sdk log 50") {
		t.Fatalf("debug logs missing: %q", res.Logs)
	}
	if !strings.Contains(res.Logs, "INFO gov24.plus info fcode log MLMWGM") {
		t.Fatalf("info logs missing: %q", res.Logs)
	}
}

func TestRunPythonLoadsSrcLayoutFromPreparedSourceRoot(t *testing.T) {
	requirePython(t)
	sourceRoot := t.TempDir()
	packageDir := filepath.Join(sourceRoot, "src", "demo_app")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "helper.py"), []byte(`VALUE = "src-layout-ok"`), 0o644); err != nil {
		t.Fatal(err)
	}
	entrypoint := filepath.Join(packageDir, "app.py")
	if err := os.WriteFile(entrypoint, []byte(`from demo_app.helper import VALUE

def main(ctx):
    return {"value": VALUE, "app": ctx.app}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), RunParams{
		ScriptLang:        "python",
		BaseDir:           t.TempDir(),
		EntrypointAbsPath: entrypoint,
		Env: []string{
			"WF_PY_SOURCE_ROOT=" + sourceRoot,
			"WF_WORKSPACE=ws-a",
			"WF_BASE_URL=http://127.0.0.1",
			"WF_TOKEN=job-token",
			"WF_APP=demo",
			"WF_ACTION=echo",
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !res.Success() {
		t.Fatalf("Run failed: result=%s logs=%s", res.Result, res.Logs)
	}
	var got map[string]string
	if err := json.Unmarshal(res.Result, &got); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if got["value"] != "src-layout-ok" || got["app"] != "demo" {
		t.Fatalf("result = %#v", got)
	}
}

func TestRunPythonInvalidInputFallsBackToEmptyObject(t *testing.T) {
	requirePython(t)
	entrypoint := filepath.Join(t.TempDir(), "main.py")
	if err := os.WriteFile(entrypoint, []byte(`
def main(ctx):
    return {"input": ctx.input}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), RunParams{
		ScriptLang:        "python",
		BaseDir:           t.TempDir(),
		EntrypointAbsPath: entrypoint,
		Input:             []byte(`{`),
		Env: []string{
			"WF_WORKSPACE=ws-a",
			"WF_BASE_URL=http://127.0.0.1",
			"WF_TOKEN=job-token",
			"WF_APP=demo",
			"WF_ACTION=echo",
		},
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !res.Success() {
		t.Fatalf("success = false, exit=%d, result=%s, logs=%s", res.ExitCode, res.Result, res.Logs)
	}
	var output struct {
		Input map[string]any `json:"input"`
	}
	if err := json.Unmarshal(res.Result, &output); err != nil {
		t.Fatalf("result is not JSON: %v", err)
	}
	if len(output.Input) != 0 {
		t.Fatalf("input = %#v, want empty object", output.Input)
	}
}

func TestRunTimeoutSynthesizesExecutionError(t *testing.T) {
	requirePython(t)
	entrypoint := filepath.Join(t.TempDir(), "main.py")
	if err := os.WriteFile(entrypoint, []byte(`
import time

def main(ctx):
    time.sleep(10)
    return {"ok": True}
`), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := Run(context.Background(), RunParams{
		ScriptLang:        "python",
		BaseDir:           t.TempDir(),
		EntrypointAbsPath: entrypoint,
		Env: []string{
			"WF_WORKSPACE=ws-a",
			"WF_BASE_URL=http://127.0.0.1",
			"WF_TOKEN=job-token",
			"WF_APP=demo",
			"WF_ACTION=echo",
		},
		Timeout: 200 * time.Millisecond,
	})
	if err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if !res.TimedOut {
		t.Fatalf("TimedOut = false, result=%s logs=%s", res.Result, res.Logs)
	}
	if res.Success() {
		t.Fatalf("Success = true for timed-out result")
	}
	var got map[string]string
	if err := json.Unmarshal(res.Result, &got); err != nil {
		t.Fatalf("timeout result is not JSON: %v", err)
	}
	if got["name"] != "ExecutionError" || got["message"] != "job timed out" {
		t.Fatalf("timeout result = %#v, want ExecutionError/job timed out", got)
	}
}

func TestRunRejectsWhitespaceScriptLangCanonically(t *testing.T) {
	_, err := Run(context.Background(), RunParams{
		ScriptLang:        " python ",
		BaseDir:           t.TempDir(),
		EntrypointAbsPath: filepath.Join(t.TempDir(), "main.py"),
	})
	if !errors.Is(err, ErrScriptLang) {
		t.Fatalf("Run error = %v, want ErrScriptLang", err)
	}
}

func TestGeneratedWrappersUseJobTokenForVariableReads(t *testing.T) {
	ts := wrapper("main.ts")
	if !strings.Contains(ts, `const transportTimeoutMs = 30000`) || !strings.Contains(ts, `controller.signal`) {
		t.Fatalf("typescript wrapper does not separate the HumanTask transport session timeout:\n%s", ts)
	}
	if strings.Contains(ts, `?app=`) {
		t.Fatalf("typescript wrapper still passes app scope to variables.get:\n%s", ts)
	}
	if strings.Contains(ts, `X-Windforce-Job-ID`) {
		t.Fatalf("typescript wrapper should not pass job identity outside WF_TOKEN:\n%s", ts)
	}
	if !strings.Contains(ts, `app: APP`) {
		t.Fatalf("typescript wrapper does not reuse APP in ctx.app:\n%s", ts)
	}
	if !strings.Contains(ts, `approval: {`) || !strings.Contains(ts, `async getResumeUrls(approver)`) ||
		!strings.Contains(ts, `flow: {`) || !strings.Contains(ts, `resumeValue: KIND === "flow_resume" ? input : undefined`) {
		t.Fatalf("typescript wrapper does not expose canonical approval/flow ctx shape:\n%s", ts)
	}
	if !strings.Contains(ts, `telemetry: { traceparent: TRACEPARENT || undefined`) ||
		!strings.Contains(ts, `headers.has("traceparent")`) ||
		!strings.Contains(ts, `headers.set("traceparent", TRACEPARENT)`) {
		t.Fatalf("typescript wrapper does not expose and propagate the W3C carrier:\n%s", ts)
	}

	py := wrapperPy("main.py")
	if strings.Contains(py, `?app=`) {
		t.Fatalf("python wrapper still passes app scope to variables.get:\n%s", py)
	}
	if strings.Contains(py, `X-Windforce-Job-ID`) {
		t.Fatalf("python wrapper should not pass job identity outside WF_TOKEN:\n%s", py)
	}
	if !strings.Contains(py, `app=_APP`) {
		t.Fatalf("python wrapper does not reuse _APP in ctx.app:\n%s", py)
	}
	if !strings.Contains(py, `class _Approval:`) || !strings.Contains(py, `async def get_resume_urls(self, approver=None):`) ||
		!strings.Contains(py, `approval=_Approval(),`) || !strings.Contains(py, `flow=SimpleNamespace(resume_value=(_input if _KIND == "flow_resume" else None))`) {
		t.Fatalf("python wrapper does not expose canonical approval/flow ctx shape:\n%s", py)
	}
	if !strings.Contains(py, `_source_root = _env("WF_PY_SOURCE_ROOT")`) ||
		!strings.Contains(py, `os.path.join(_source_root, "src")`) {
		t.Fatalf("python wrapper does not add source root/src layout import paths:\n%s", py)
	}
	if !strings.Contains(py, `telemetry=SimpleNamespace(`) ||
		!strings.Contains(py, `headers["traceparent"] = _TRACEPARENT`) {
		t.Fatalf("python wrapper does not expose and propagate the W3C carrier:\n%s", py)
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(value); err != nil {
		panic(err)
	}
}

func TestDefaultWindowsPythonPathSkipsWindowsAppsAlias(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows path resolution only")
	}
	tempDir := t.TempDir()
	windowsApps := filepath.Join(tempDir, "Microsoft", "WindowsApps")
	realBin := filepath.Join(tempDir, "Python", "bin")
	if err := os.MkdirAll(windowsApps, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(realBin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(windowsApps, "python.exe"), []byte("alias"), 0o755); err != nil {
		t.Fatal(err)
	}
	realPython := filepath.Join(realBin, "python.exe")
	if err := os.WriteFile(realPython, []byte("real"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", windowsApps+string(os.PathListSeparator)+realBin)

	if got := defaultWindowsPythonPath(); got != realPython {
		t.Fatalf("defaultWindowsPythonPath() = %q, want %q", got, realPython)
	}
}

func requirePython(t *testing.T) {
	t.Helper()
	python := "python3"
	if runtime.GOOS == "windows" {
		if defaultWindowsPythonPath() != "" {
			return
		}
		if _, err := exec.LookPath("py"); err == nil {
			return
		}
		python = "python"
	}
	if _, err := exec.LookPath(python); err != nil {
		t.Skipf("%s not found in PATH", python)
	}
}
