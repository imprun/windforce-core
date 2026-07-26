package state

import "time"

type nowFunc func() time.Time

func currentUTC(now nowFunc) time.Time {
	if now != nil {
		return now().UTC()
	}
	return time.Now().UTC()
}
