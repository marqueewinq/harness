//go:build !linux && !darwin

package dashboard

import "time"

func birthTime(string) (time.Time, bool) { return time.Time{}, false }
