package diagnose

import (
	"fmt"
	"io"
	"strings"
	"time"
)

const ReportSchemaVersion = "diagnose/v1"

type Status string

const (
	StatusPass        Status = "pass"
	StatusWarn        Status = "warn"
	StatusFail        Status = "fail"
	StatusUnsupported Status = "unsupported"
)

type Mode string

const (
	ModeStandalone   Mode = "standalone"
	ModeServer       Mode = "server"
	ModeRemoteWorker Mode = "remote-worker"
)

type Check struct {
	ID          string         `json:"id"`
	Status      Status         `json:"status"`
	Message     string         `json:"message"`
	Remediation string         `json:"remediation,omitempty"`
	ObservedAt  time.Time      `json:"observed_at"`
	Details     map[string]any `json:"details,omitempty"`
}

type Report struct {
	SchemaVersion string    `json:"schema_version"`
	Mode          Mode      `json:"mode"`
	Status        Status    `json:"status"`
	ObservedAt    time.Time `json:"observed_at"`
	Checks        []Check   `json:"checks"`
}

func ParseMode(raw string) (Mode, error) {
	mode := Mode(strings.TrimSpace(raw))
	switch mode {
	case ModeStandalone, ModeServer, ModeRemoteWorker:
		return mode, nil
	default:
		return "", fmt.Errorf("unsupported diagnose mode %q", raw)
	}
}

// ExitCode is stable for automation: 0=pass, 1=warn, 2=fail.
func ExitCode(report Report) int {
	switch report.Status {
	case StatusFail:
		return 2
	case StatusWarn:
		return 1
	default:
		return 0
	}
}

func WriteText(writer io.Writer, report Report) error {
	if _, err := fmt.Fprintf(writer, "Windforce Core diagnose: %s (%s)\n", strings.ToUpper(string(report.Status)), report.Mode); err != nil {
		return err
	}
	for _, check := range report.Checks {
		if _, err := fmt.Fprintf(writer, "%-11s %-32s %s\n", strings.ToUpper(string(check.Status)), check.ID, check.Message); err != nil {
			return err
		}
		if check.Remediation != "" && (check.Status == StatusWarn || check.Status == StatusFail) {
			if _, err := fmt.Fprintf(writer, "            remediation: %s\n", check.Remediation); err != nil {
				return err
			}
		}
	}
	return nil
}

func overallStatus(checks []Check) Status {
	status := StatusPass
	for _, check := range checks {
		switch check.Status {
		case StatusFail:
			return StatusFail
		case StatusWarn:
			status = StatusWarn
		}
	}
	return status
}
