package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/imprun/windforce-core/internal/contract"
	actionruntime "github.com/imprun/windforce-core/internal/runtime"
	"github.com/imprun/windforce-core/internal/secretmask"
	"github.com/imprun/windforce-core/internal/state"
)

type Runner interface {
	Run(ctx context.Context, request actionruntime.RunRequest) (contract.JobResult, error)
}

type Processor struct {
	Store    Backend
	Runner   Runner
	WorkerID string
	Group    string
	Tags     []string
	// Labels is the capability label set this worker offers (ADR 0009).
	Labels []string
	// Slots is the worker concurrency cap advertised to the registry.
	Slots             int
	EgressProxyAddr   string
	LeaseTTL          time.Duration
	DrainTimeout      time.Duration
	HeartbeatInterval time.Duration
	LogFlushInterval  time.Duration
	LogCapBytes       int
	LogJobPayloads    bool
	TeeJobLogs        bool
	RuntimeBindings   RuntimeBindings
	RuntimeResolver   RuntimeInputResolver
}

type RuntimeInputResolver interface {
	ResolveRuntimeInput(ctx context.Context, job state.Job, input json.RawMessage) (json.RawMessage, []string, error)
}

// workerID resolves a stable identity for both the claim path and the
// registry lifecycle.
func (p *Processor) workerID() string {
	if p.WorkerID == "" {
		p.WorkerID = state.NewID("worker")
	}
	return p.WorkerID
}

func (p *Processor) ProcessOne(ctx context.Context) (bool, error) {
	return p.processOne(ctx, ctx)
}

func (p *Processor) processOne(claimCtx context.Context, executionCtx context.Context) (bool, error) {
	if p.Store == nil {
		return false, errors.New("state store is required")
	}
	workerID := p.workerID()
	job, lease, err := p.Store.ClaimJobForWorker(claimCtx, workerID, p.Tags, p.Labels, p.LeaseTTL)
	if errors.Is(err, state.ErrNoQueuedJob) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	startedAt := time.Now()
	outcome := "running"
	jobError := ""
	log.Printf("worker job started job=%s app=%s action=%s", job.ID, job.Payload.App, job.Payload.Action)
	defer func() {
		log.Printf("worker job finished job=%s app=%s action=%s outcome=%s duration=%s error=%q",
			job.ID, job.Payload.App, job.Payload.Action, outcome, time.Since(startedAt).Round(time.Millisecond), jobError)
	}()

	workspaceID := job.Payload.Workspace
	if workspaceID == "" {
		workspaceID = job.Payload.PinnedDeployment().SourceWorkspace()
	}
	workspaceID = contract.NormalizeWorkspace(workspaceID)
	runCtx, cancel := context.WithCancel(executionCtx)
	defer cancel()
	if provider, ok := p.Store.(ExecutionContextProvider); ok {
		runCtx = provider.WithExecutionContext(runCtx, job, lease)
	}
	completeCtx := context.WithoutCancel(executionCtx)
	heartbeat := p.startHeartbeat(lease, cancel)
	defer heartbeat.stop()
	input, err := p.Store.DecryptInput(runCtx, workspaceID, job.Payload.Input)
	if err != nil {
		outcome = "failed"
		jobError = "could not decrypt job input"
		result := contract.JobResult{
			JobID:    job.ID,
			App:      job.Payload.App,
			Action:   job.Payload.Action,
			Output:   actionruntime.ErrorResult("InputDecryptError", "could not decrypt job input"),
			ExitCode: -1,
			Error:    "could not decrypt job input",
		}
		return completeProcessed(p.Store.CompleteJobFailed(completeCtx, lease, result))
	}
	if !job.Payload.InputConfigResolved {
		input, err = p.Store.ResolveInput(runCtx, workspaceID, job.Payload.App, job.Payload.Action, job.Payload.ClientID, input)
		if err != nil {
			outcome = "failed"
			name := "InputConfigError"
			message := "could not resolve input settings"
			var locked *state.LockedKeysError
			if errors.As(err, &locked) {
				name = "InputConfigLocked"
				message = locked.Error()
			}
			jobError = message
			result := contract.JobResult{
				JobID:    job.ID,
				App:      job.Payload.App,
				Action:   job.Payload.Action,
				Output:   actionruntime.ErrorResult(name, message),
				ExitCode: -1,
				Error:    message,
			}
			return completeProcessed(p.Store.CompleteJobFailed(completeCtx, lease, result))
		}
	}
	var secretValues []string
	if p.RuntimeResolver != nil {
		input, secretValues, err = p.RuntimeResolver.ResolveRuntimeInput(runCtx, job, input)
		if err != nil {
			outcome = "failed"
			jobError = "could not resolve runtime configuration"
			result := contract.JobResult{
				JobID:    job.ID,
				App:      job.Payload.App,
				Action:   job.Payload.Action,
				Output:   actionruntime.ErrorResult("RuntimeConfigurationError", jobError),
				ExitCode: -1,
				Error:    jobError,
			}
			return completeProcessed(p.Store.CompleteJobFailed(completeCtx, lease, result))
		}
	}
	if provider, ok := p.Store.(SecretValueProvider); ok {
		secretValues = append(secretValues, provider.SecretValuesFor(job.ID)...)
	}
	logJobInput(p.LogJobPayloads, job.ID, job.Payload.App, job.Payload.Action, input, secretValues)
	input = state.StripReservedRuntimeInput(input)
	input, err = p.RuntimeBindings.Apply(input)
	if err != nil {
		outcome = "failed"
		jobError = "could not apply runtime bindings"
		result := contract.JobResult{
			JobID:    job.ID,
			App:      job.Payload.App,
			Action:   job.Payload.Action,
			Output:   actionruntime.ErrorResult("RuntimeBindingError", "could not apply runtime bindings"),
			ExitCode: -1,
			Error:    "could not apply runtime bindings",
		}
		return completeProcessed(p.Store.CompleteJobFailed(completeCtx, lease, result))
	}
	jobToken := ""
	if provider, ok := p.Store.(JobTokenProvider); ok {
		jobToken = provider.JobTokenFor(job.ID)
	}
	emitLog := func(chunk []byte) {
		_ = p.Store.AppendLogs(context.Background(), job.ID, workspaceID, string(chunk))
		if p.TeeJobLogs {
			log.Printf("worker job log job=%s app=%s action=%s chunk=%q", job.ID, job.Payload.App, job.Payload.Action, string(chunk))
		}
	}
	maskedLogs := secretmask.NewStream(secretValues, emitLog)
	result, runErr := p.Runner.Run(runCtx, actionruntime.RunRequest{
		JobID:           job.ID,
		Attempt:         job.Attempt,
		WorkspaceID:     workspaceID,
		Deployment:      job.Payload.PinnedDeployment(),
		Action:          job.Payload.Action,
		Input:           input,
		TriggerKind:     job.Payload.TriggerKind,
		TriggerHeaders:  job.Payload.TriggerHeaders,
		ScheduledFor:    job.Payload.ScheduledFor,
		Tag:             job.Payload.Tag,
		CreatedBy:       job.Payload.CreatedBy,
		PermissionedAs:  job.Payload.PermissionedAs,
		JobToken:        jobToken,
		WorkerGroup:     p.Group,
		EgressProxyAddr: p.EgressProxyAddr,
		LogSink: func(chunk []byte) {
			maskedLogs.Write(chunk)
		},
		LogFlushInterval: p.LogFlushInterval,
		LogCapBytes:      p.LogCapBytes,
	})
	if result.Interruption == nil {
		result.Interruption = heartbeat.interruptionCause()
	}
	if result.Interruption == nil && executionCtx.Err() != nil {
		result.Interruption = &contract.ExecutionInterruption{
			Cause:    contract.InterruptionWorkerShutdown,
			Source:   "worker",
			Message:  "worker execution context was canceled",
			Observed: time.Now().UTC(),
		}
	}
	maskedLogs.Flush()
	result = maskJobResult(result, secretValues)
	logJobExecution(p.LogJobPayloads, job.ID, job.Payload.App, job.Payload.Action, result, secretValues)
	result.JobID = job.ID
	result.Stdout = ""
	result.Stderr = ""
	if runErr != nil {
		if result.Error == "" {
			result.Error = secretmask.String(runErr.Error(), secretValues)
		}
		if len(result.Output) == 0 {
			result.Output = namedErrorResult(runErr, result.Error)
		}
		outcome = "failed"
		jobError = result.Error
		return completeProcessed(p.Store.CompleteJobFailed(completeCtx, lease, result))
	}
	if result.ExitCode != 0 {
		if result.Error == "" {
			result.Error = fmt.Sprintf("action exited with code %d", result.ExitCode)
		}
		if len(result.Output) == 0 {
			result.Output = actionruntime.ErrorResult("ExecutionError", result.Error)
		}
		outcome = "failed"
		jobError = result.Error
		return completeProcessed(p.Store.CompleteJobFailed(completeCtx, lease, result))
	}
	if message, failed := actionDeclaredFailure(result); failed {
		result.Error = message
		outcome = "failed"
		jobError = result.Error
		return completeProcessed(p.Store.CompleteJobFailed(completeCtx, lease, result))
	}

	task, ok, err := HumanTaskFromOutput(job.RunID, result.Output)
	if err != nil {
		result.Error = err.Error()
		outcome = "failed"
		jobError = result.Error
		return completeProcessed(p.Store.CompleteJobFailed(completeCtx, lease, result))
	}
	if ok {
		outcome = "waiting_human"
		return completeProcessed(p.Store.CompleteJobWaitingHuman(completeCtx, lease, result, task))
	}
	outcome = "succeeded"
	return completeProcessed(p.Store.CompleteJobSucceeded(completeCtx, lease, result))
}

func logJobInput(enabled bool, jobID string, app string, action string, input []byte, secrets []string) {
	if enabled {
		log.Printf("worker job input job=%s app=%s action=%s payload=%s", jobID, app, action, secretmask.Bytes(input, secrets))
	}
}

func logJobExecution(enabled bool, jobID string, app string, action string, result contract.JobResult, secrets []string) {
	if enabled {
		log.Printf("worker job execution job=%s app=%s action=%s exit_code=%d stdout=%q stderr=%q output=%s",
			jobID, app, action, result.ExitCode,
			secretmask.String(result.Stdout, secrets),
			secretmask.String(result.Stderr, secrets),
			secretmask.Bytes(result.Output, secrets))
	}
}

func maskJobResult(result contract.JobResult, secrets []string) contract.JobResult {
	result.Output = secretmask.Bytes(result.Output, secrets)
	result.Stdout = secretmask.String(result.Stdout, secrets)
	result.Stderr = secretmask.String(result.Stderr, secrets)
	result.Error = secretmask.String(result.Error, secrets)
	return result
}

type heartbeatMonitor struct {
	done chan struct{}
	once sync.Once
	mu   sync.RWMutex
	why  *contract.ExecutionInterruption
}

func (m *heartbeatMonitor) stop() {
	m.once.Do(func() { close(m.done) })
}

func (m *heartbeatMonitor) interrupt(cause string, message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.why != nil {
		return
	}
	m.why = &contract.ExecutionInterruption{
		Cause:    cause,
		Source:   "worker_heartbeat",
		Message:  message,
		Observed: time.Now().UTC(),
	}
}

func (m *heartbeatMonitor) interruptionCause() *contract.ExecutionInterruption {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.why == nil {
		return nil
	}
	cloned := *m.why
	return &cloned
}

func (p *Processor) startHeartbeat(lease state.Lease, cancel context.CancelFunc) *heartbeatMonitor {
	interval := p.effectiveHeartbeatInterval()
	monitor := &heartbeatMonitor{done: make(chan struct{})}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-monitor.done:
				return
			case <-ticker.C:
				heartbeat, err := p.Store.HeartbeatJob(context.Background(), lease, p.LeaseTTL)
				if err != nil {
					log.Printf("worker heartbeat job %s: %v", lease.JobID, err)
					continue
				}
				if heartbeat.CanceledBy != nil {
					message := "run canceled by " + *heartbeat.CanceledBy
					if heartbeat.CanceledReason != nil && *heartbeat.CanceledReason != "" {
						message = *heartbeat.CanceledReason
					}
					monitor.interrupt(contract.InterruptionRunCanceled, message)
					cancel()
					return
				}
				if !heartbeat.StillOwned {
					monitor.interrupt(contract.InterruptionLeaseLost, "job lease is no longer owned by this worker")
					cancel()
					return
				}
			}
		}
	}()
	return monitor
}

func (p *Processor) effectiveHeartbeatInterval() time.Duration {
	if p.HeartbeatInterval > 0 {
		return p.HeartbeatInterval
	}
	if p.LeaseTTL > 0 {
		interval := p.LeaseTTL / 3
		if interval < 10*time.Millisecond {
			return 10 * time.Millisecond
		}
		if interval > 10*time.Second {
			return 10 * time.Second
		}
		return interval
	}
	return 10 * time.Second
}

func completeProcessed(err error) (bool, error) {
	if errors.Is(err, state.ErrInvalidLease) {
		return true, nil
	}
	return true, err
}

func namedErrorResult(err error, message string) json.RawMessage {
	name := "ExecutionError"
	if runtimeName, ok := actionruntime.ErrorName(err); ok {
		name = runtimeName
	}
	return actionruntime.ErrorResult(name, message)
}

// heartbeatInterval keeps the registry entry fresh well inside
// state.WorkerLiveTTL.
const heartbeatInterval = 15 * time.Second

func (p *Processor) RunLoop(ctx context.Context, pollInterval time.Duration) error {
	if pollInterval <= 0 {
		pollInterval = 500 * time.Millisecond
	}
	drainTimeout := p.DrainTimeout
	if drainTimeout <= 0 {
		drainTimeout = 30 * time.Second
	}
	workerID := p.workerID()
	record := state.WorkerRecord{
		ID:     workerID,
		Group:  p.Group,
		Tags:   append([]string(nil), p.Tags...),
		Labels: append([]string(nil), p.Labels...),
		Slots:  p.Slots,
		Status: state.WorkerStatusActive,
	}
	if err := p.Store.RegisterWorker(ctx, record); err != nil {
		return fmt.Errorf("register worker: %w", err)
	}
	log.Printf("worker lifecycle worker=%s status=%s", workerID, state.WorkerStatusActive)

	// Registry heartbeats use a lifetime context that survives the caller's
	// cancellation while an admitted Job drains.
	heartbeatCtx, stopHeartbeat := context.WithCancel(context.Background())
	transitionDone := make(chan struct{})
	loopDone := make(chan struct{})
	go func() {
		defer close(transitionDone)
		select {
		case <-ctx.Done():
			draining := record
			draining.Status = state.WorkerStatusDraining
			updateCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := p.Store.RegisterWorker(updateCtx, draining); err != nil {
				log.Printf("worker lifecycle worker=%s status=%s: %v", workerID, state.WorkerStatusDraining, err)
				return
			}
			log.Printf("worker lifecycle worker=%s status=%s timeout=%s", workerID, state.WorkerStatusDraining, drainTimeout)
		case <-loopDone:
		}
	}()
	defer func() {
		close(loopDone)
		stopHeartbeat()
		<-transitionDone
		if err := p.Store.DeregisterWorker(context.Background(), workerID); err != nil {
			log.Printf("deregister worker %s: %v", workerID, err)
		}
		log.Printf("worker lifecycle worker=%s status=offline", workerID)
	}()

	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()
		for {
			select {
			case <-heartbeatCtx.Done():
				return
			case <-ticker.C:
				err := p.Store.HeartbeatWorker(heartbeatCtx, workerID)
				if err == nil {
					continue
				}
				log.Printf("worker heartbeat %s: %v", workerID, err)
				if errors.Is(err, state.ErrNotFound) && heartbeatCtx.Err() == nil && ctx.Err() == nil {
					if regErr := p.Store.RegisterWorker(heartbeatCtx, record); regErr != nil {
						log.Printf("re-register worker %s: %v", workerID, regErr)
					}
				}
			}
		}
	}()

	// Transient failures (engine restart, network blip, expired lease on
	// complete) must not kill a long-running worker — back off and retry.
	consecutiveFailures := 0
	for {
		executionCtx, cancelExecution := context.WithCancel(context.WithoutCancel(ctx))
		stopDrainCancellation := context.AfterFunc(ctx, func() {
			timer := time.NewTimer(drainTimeout)
			defer timer.Stop()
			select {
			case <-timer.C:
				log.Printf("worker drain timeout worker=%s timeout=%s; canceling active job", workerID, drainTimeout)
				cancelExecution()
			case <-executionCtx.Done():
			}
		})
		processed, err := p.processOne(ctx, executionCtx)
		cancelExecution()
		stopDrainCancellation()

		if ctx.Err() != nil {
			<-transitionDone
			return nil
		}
		if err != nil {
			consecutiveFailures++
			delay := retryDelay(pollInterval, consecutiveFailures)
			log.Printf("worker %s: process: %v (retry in %s)", workerID, err, delay)
			select {
			case <-ctx.Done():
				<-transitionDone
				return nil
			case <-time.After(delay):
			}
			continue
		}
		consecutiveFailures = 0
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			<-transitionDone
			return nil
		case <-time.After(pollInterval):
		}
	}
}

// retryDelay backs off exponentially from the poll interval to 30s.
func retryDelay(pollInterval time.Duration, failures int) time.Duration {
	const maxDelay = 30 * time.Second
	delay := pollInterval
	for i := 1; i < failures; i++ {
		delay *= 2
		if delay >= maxDelay {
			return maxDelay
		}
	}
	if delay > maxDelay {
		return maxDelay
	}
	return delay
}

func HumanTaskFromOutput(runID string, output json.RawMessage) (state.HumanTask, bool, error) {
	if len(output) == 0 {
		return state.HumanTask{}, false, nil
	}
	var envelope struct {
		Windforce *struct {
			Type        string          `json:"type"`
			Title       string          `json:"title"`
			Description string          `json:"description"`
			Fields      json.RawMessage `json:"fields"`
			TimeoutMs   int64           `json:"timeoutMs"`
		} `json:"$windforce"`
	}
	if err := json.Unmarshal(output, &envelope); err != nil {
		return state.HumanTask{}, false, err
	}
	if envelope.Windforce == nil {
		return state.HumanTask{}, false, nil
	}
	if envelope.Windforce.Type != "human_task" {
		return state.HumanTask{}, false, fmt.Errorf("unsupported $windforce type %q", envelope.Windforce.Type)
	}
	title := envelope.Windforce.Title
	if title == "" {
		title = "Human task"
	}
	var expiresAt *time.Time
	if envelope.Windforce.TimeoutMs > 0 {
		value := time.Now().UTC().Add(time.Duration(envelope.Windforce.TimeoutMs) * time.Millisecond)
		expiresAt = &value
	}
	return state.HumanTask{
		ID:          state.NewID("human"),
		RunID:       runID,
		State:       state.HumanTaskPending,
		Title:       title,
		Description: envelope.Windforce.Description,
		Schema:      fieldsSchema(envelope.Windforce.Fields),
		ExpiresAt:   expiresAt,
	}, true, nil
}

func fieldsSchema(fields json.RawMessage) json.RawMessage {
	if len(fields) == 0 {
		return nil
	}
	data, err := json.Marshal(map[string]json.RawMessage{"fields": fields})
	if err != nil {
		return nil
	}
	return data
}

func ResultError(result contract.JobResult) string {
	if result.Error != "" {
		return result.Error
	}
	if result.ExitCode != 0 {
		return fmt.Sprintf("action exited with code %d", result.ExitCode)
	}
	return ""
}
