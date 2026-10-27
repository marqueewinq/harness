package internal

import (
	"fmt"
	"time"
)

// FormatDuration renders d compactly with its two largest units, truncated:
// 30s, 3m12s, 1h29m, 2d3h. A zero lower unit is dropped (1m, 2h, 1d).
func FormatDuration(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	s := int64(d / time.Second)
	day, hour, min := int64(24*3600), int64(3600), int64(60)
	pair := func(big int64, bigUnit string, small int64, smallUnit string) string {
		if small == 0 {
			return fmt.Sprintf("%d%s", big, bigUnit)
		}
		return fmt.Sprintf("%d%s%d%s", big, bigUnit, small, smallUnit)
	}
	switch {
	case s >= day:
		return pair(s/day, "d", s%day/hour, "h")
	case s >= hour:
		return pair(s/hour, "h", s%hour/min, "m")
	case s >= min:
		return pair(s/min, "m", s%min, "s")
	}
	return fmt.Sprintf("%ds", s)
}

// TimeInStates gives, for transitions entered at times (oldest first), how
// long the subject stayed in each entered state: until the next transition,
// and for the last (current) state until now.
func TimeInStates(times []time.Time, now time.Time) []time.Duration {
	if len(times) == 0 {
		return nil
	}
	out := make([]time.Duration, len(times))
	for i, t := range times {
		end := now
		if i+1 < len(times) {
			end = times[i+1]
		}
		out[i] = end.Sub(t)
	}
	return out
}

// FormatStay renders a TimeInStates entry; the current state's is ongoing.
func FormatStay(d time.Duration, ongoing bool) string {
	if ongoing {
		return FormatDuration(d) + " (now)"
	}
	return FormatDuration(d)
}
