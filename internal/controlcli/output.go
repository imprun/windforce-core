package controlcli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"text/template"

	"github.com/itchyny/gojq"
)

type terminalWriter interface {
	IsTerminal() bool
}

func isTerminalOutput(writer io.Writer) bool {
	if terminal, ok := writer.(terminalWriter); ok {
		return terminal.IsTerminal()
	}
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (r *runner) writeOutput(value any) error {
	decoded, err := normalizeOutputValue(value)
	if err != nil {
		return err
	}
	if r.outputFields != "" {
		decoded, err = selectOutputFields(decoded, r.outputFields)
		if err != nil {
			return err
		}
	}
	if r.jqExpression != "" {
		return writeJQOutput(r.stdout, decoded, r.jqExpression)
	}
	if r.outputTemplate != "" {
		return writeTemplateOutput(r.stdout, decoded, r.outputTemplate)
	}
	if r.humanOutput {
		return writeHumanOutput(r.stdout, decoded)
	}

	var data []byte
	if r.pretty {
		data, err = json.MarshalIndent(decoded, "", "  ")
	} else {
		data, err = json.Marshal(decoded)
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(r.stdout, string(data))
	return err
}

func normalizeOutputValue(value any) (any, error) {
	if raw, ok := value.(json.RawMessage); ok {
		var decoded any
		if err := json.Unmarshal(raw, &decoded); err != nil {
			return nil, err
		}
		return decoded, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return nil, err
	}
	return decoded, nil
}

func selectOutputFields(value any, specification string) (any, error) {
	if strings.TrimSpace(specification) == "*" {
		return value, nil
	}
	fields := splitOutputFields(specification)
	if len(fields) == 0 {
		return nil, usageError{"--json requires at least one field or *"}
	}
	switch typed := value.(type) {
	case map[string]any:
		return selectMapFields(typed, fields)
	case []any:
		rows := make([]any, 0, len(typed))
		seen := make(map[string]bool, len(fields))
		for _, item := range typed {
			row, ok := item.(map[string]any)
			if !ok {
				return nil, usageError{"--json fields can only select object properties"}
			}
			selected := make(map[string]any, len(fields))
			for _, field := range fields {
				if property, exists := row[field]; exists {
					selected[field] = property
					seen[field] = true
				} else {
					selected[field] = nil
				}
			}
			rows = append(rows, selected)
		}
		for _, field := range fields {
			if !seen[field] && len(typed) > 0 {
				return nil, unknownOutputField(field, availableFieldsFromRows(typed))
			}
		}
		return rows, nil
	default:
		return nil, usageError{"--json fields require an object or array of objects"}
	}
}

func selectMapFields(value map[string]any, fields []string) (map[string]any, error) {
	selected := make(map[string]any, len(fields))
	for _, field := range fields {
		property, ok := value[field]
		if !ok {
			return nil, unknownOutputField(field, sortedMapKeys(value))
		}
		selected[field] = property
	}
	return selected, nil
}

func splitOutputFields(specification string) []string {
	seen := map[string]bool{}
	var fields []string
	for _, field := range strings.Split(specification, ",") {
		field = strings.TrimSpace(field)
		if field != "" && !seen[field] {
			fields = append(fields, field)
			seen[field] = true
		}
	}
	return fields
}

func unknownOutputField(field string, available []string) error {
	if len(available) == 0 {
		return usageError{fmt.Sprintf("unknown JSON field %q", field)}
	}
	return usageError{fmt.Sprintf("unknown JSON field %q; available fields: %s", field, strings.Join(available, ", "))}
}

func availableFieldsFromRows(rows []any) []string {
	keys := map[string]bool{}
	for _, item := range rows {
		if row, ok := item.(map[string]any); ok {
			for key := range row {
				keys[key] = true
			}
		}
	}
	result := make([]string, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}

func writeJQOutput(writer io.Writer, value any, expression string) error {
	query, err := gojq.Parse(expression)
	if err != nil {
		return usageError{fmt.Sprintf("invalid --jq expression: %v", err)}
	}
	code, err := gojq.Compile(query)
	if err != nil {
		return usageError{fmt.Sprintf("compile --jq expression: %v", err)}
	}
	iterator := code.Run(value)
	for {
		result, ok := iterator.Next()
		if !ok {
			return nil
		}
		if queryErr, ok := result.(error); ok {
			return usageError{fmt.Sprintf("--jq evaluation failed: %v", queryErr)}
		}
		if text, ok := result.(string); ok {
			if _, err := fmt.Fprintln(writer, text); err != nil {
				return err
			}
			continue
		}
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintln(writer, string(data)); err != nil {
			return err
		}
	}
}

func writeTemplateOutput(writer io.Writer, value any, source string) error {
	compiled, err := template.New("output").
		Option("missingkey=error").
		Funcs(template.FuncMap{
			"json": func(value any) (string, error) {
				data, err := json.Marshal(value)
				return string(data), err
			},
			"join": strings.Join,
		}).
		Parse(source)
	if err != nil {
		return usageError{fmt.Sprintf("invalid --template: %v", err)}
	}
	var rendered bytes.Buffer
	if err := compiled.Execute(&rendered, value); err != nil {
		return usageError{fmt.Sprintf("execute --template: %v", err)}
	}
	data := rendered.Bytes()
	if _, err := writer.Write(data); err != nil {
		return err
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		_, err = fmt.Fprintln(writer)
	}
	return err
}

func writeHumanOutput(writer io.Writer, value any) error {
	switch typed := value.(type) {
	case map[string]any:
		table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
		for _, key := range orderedOutputKeys(typed) {
			if _, err := fmt.Fprintf(table, "%s\t%s\n", humanHeading(key), humanValue(typed[key])); err != nil {
				return err
			}
		}
		return table.Flush()
	case []any:
		return writeHumanRows(writer, typed)
	default:
		_, err := fmt.Fprintln(writer, humanValue(typed))
		return err
	}
}

func writeHumanRows(writer io.Writer, rows []any) error {
	if len(rows) == 0 {
		_, err := fmt.Fprintln(writer, "No results.")
		return err
	}
	first, ok := rows[0].(map[string]any)
	if !ok {
		for _, row := range rows {
			if _, err := fmt.Fprintln(writer, humanValue(row)); err != nil {
				return err
			}
		}
		return nil
	}
	keys := orderedOutputKeys(first)
	table := tabwriter.NewWriter(writer, 0, 4, 2, ' ', 0)
	for index, key := range keys {
		if index > 0 {
			_, _ = fmt.Fprint(table, "\t")
		}
		_, _ = fmt.Fprint(table, humanHeading(key))
	}
	_, _ = fmt.Fprintln(table)
	for _, item := range rows {
		row, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("cannot render mixed result types as a table")
		}
		for index, key := range keys {
			if index > 0 {
				_, _ = fmt.Fprint(table, "\t")
			}
			_, _ = fmt.Fprint(table, humanValue(row[key]))
		}
		_, _ = fmt.Fprintln(table)
	}
	return table.Flush()
}

func orderedOutputKeys(value map[string]any) []string {
	preferred := []string{
		"name", "app", "id", "run_id", "state", "active", "commit", "commit_sha",
		"release_id", "deployment_id", "bundle_status", "workspace", "context",
		"api_url", "current", "created_at", "updated_at",
	}
	seen := make(map[string]bool, len(value))
	keys := make([]string, 0, len(value))
	for _, key := range preferred {
		if _, ok := value[key]; ok {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	remaining := make([]string, 0, len(value)-len(keys))
	for key := range value {
		if !seen[key] {
			remaining = append(remaining, key)
		}
	}
	sort.Strings(remaining)
	return append(keys, remaining...)
}

func sortedMapKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func humanHeading(key string) string {
	return strings.ToUpper(strings.ReplaceAll(key, "_", " "))
}

func humanValue(value any) string {
	switch typed := value.(type) {
	case nil:
		return "-"
	case string:
		if typed == "" {
			return "-"
		}
		return typed
	case bool:
		if typed {
			return "yes"
		}
		return "no"
	default:
		data, err := json.Marshal(value)
		if err != nil {
			return fmt.Sprint(value)
		}
		return string(data)
	}
}
