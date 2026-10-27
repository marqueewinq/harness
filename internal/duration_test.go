package internal

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		-5 * time.Second:                      "0s",
		0:                                     "0s",
		999 * time.Millisecond:                "0s",
		30 * time.Second:                      "30s",
		59*time.Second + 999*time.Millisecond: "59s",
		time.Minute:                           "1m",
		3*time.Minute + 12*time.Second:        "3m12s",
		59*time.Minute + 59*time.Second:       "59m59s",
		time.Hour:                             "1h",
		time.Hour + 29*time.Minute + 59*time.Second: "1h29m",
		23*time.Hour + 59*time.Minute:               "23h59m",
		24 * time.Hour:                              "1d",
		51*time.Hour + 30*time.Minute:               "2d3h",
		400 * 24 * time.Hour:                        "400d",
	} {
		if got := FormatDuration(d); got != want {
			t.Errorf("FormatDuration(%v) = %q, want %q", d, got, want)
		}
	}
}

func TestTimeInStates(t *testing.T) {
	t0 := time.Date(2026, 10, 27, 17, 31, 7, 0, time.UTC)
	at := []time.Time{t0, t0.Add(51 * time.Second), t0.Add(90 * time.Minute)}
	got := TimeInStates(at, t0.Add(102*time.Minute))
	want := []string{"51s", "1h29m", "12m (now)"}
	var strs []string
	for i, d := range got {
		strs = append(strs, FormatStay(d, i == len(got)-1))
	}
	if !reflect.DeepEqual(strs, want) {
		t.Errorf("stays = %v, want %v", strs, want)
	}
	if TimeInStates(nil, t0) != nil {
		t.Error("no transitions should give no stays")
	}
}

func TestWorkStatusShowsTimeInState(t *testing.T) {
	app, task := setupTaskInState(t, StateInProgress)
	for _, s := range []TaskState{StateChecksRunning, StateChecksFailed} {
		if err := app.DB.UpdateTaskState(task.ID, s); err != nil {
			t.Fatal(err)
		}
	}
	out, err := captureStdout(t, func() error { return app.CmdWorkStatus(task.ID) })
	if err != nil {
		t.Fatal(err)
	}
	i := strings.Index(out, "State history:")
	if i < 0 {
		t.Fatalf("no history:\n%s", out)
	}
	lines := strings.Split(strings.TrimSpace(out[i:]), "\n")[1:4]
	for n, l := range lines {
		if n < 2 && !strings.HasSuffix(l, "0s") {
			t.Errorf("line %d lacks its stay: %q", n, l)
		}
	}
	if !strings.HasSuffix(lines[2], "(now)") || !strings.Contains(lines[2], "CHECKS_RUNNING -> CHECKS_FAILED") {
		t.Errorf("current state line = %q", lines[2])
	}
}
