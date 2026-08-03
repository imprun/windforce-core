package state

import "unicode/utf8"

func logWindow(logs string, afterOffset int64, limitBytes int) JobLogUpdate {
	data := []byte(logs)
	if afterOffset < 0 {
		afterOffset = 0
	}
	if afterOffset > int64(len(data)) {
		afterOffset = int64(len(data))
	}
	start := int(afterOffset)
	for start < len(data) && !utf8.RuneStart(data[start]) {
		start++
	}
	end := len(data)
	if limitBytes > 0 && end-start > limitBytes {
		end = start + limitBytes
		for end > start && end < len(data) && !utf8.RuneStart(data[end]) {
			end--
		}
		if end == start {
			_, size := utf8.DecodeRune(data[start:])
			end = start + size
		}
	}
	return JobLogUpdate{NewLogs: string(data[start:end]), Offset: int64(end)}
}
