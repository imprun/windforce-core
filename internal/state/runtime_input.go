package state

import "encoding/json"

const ReservedRuntimeInputKey = "_SCRAPING_RUNTIME"

func ContainsReservedRuntimeInput(config map[string]json.RawMessage) bool {
	_, exists := config[ReservedRuntimeInputKey]
	return exists
}

func StripReservedRuntimeInput(input json.RawMessage) json.RawMessage {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(input, &object); err != nil || object == nil {
		return input
	}
	if _, exists := object[ReservedRuntimeInputKey]; !exists {
		return input
	}
	delete(object, ReservedRuntimeInputKey)
	clean, err := json.Marshal(object)
	if err != nil {
		return input
	}
	return clean
}
