package reqtui

import "time"

// timeNowString returns the current wall clock as "HH:MM:SS" for the header
// timestamp. It is a thin wrapper to keep tests deterministic: tests can
// replace timeNow.
var timeNow = time.Now

func timeNowString() string {
	return timeNow().Format("15:04:05")
}
