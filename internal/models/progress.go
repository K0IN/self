package models

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"
)

// TerminalProgress renders a progress block like:
//
//	1.84 GiB / 2.31 GiB
//	███████████████████████████████░░░░░░ 79%
//	142 MiB/s   ETA 3s
//
// On non-TTY writers it prints a line every few seconds instead.
type TerminalProgress struct {
	W   io.Writer
	TTY bool

	mu        sync.Mutex
	name      string
	total     int64
	startDone int64
	done      int64
	started   time.Time
	lastDraw  time.Time
	drawn     bool
	// speed smoothing
	sampleT time.Time
	sampleB int64
	speed   float64
	now     func() time.Time
}

func (t *TerminalProgress) clock() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

func (t *TerminalProgress) Start(name string, done, total int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.name, t.total, t.startDone, t.done = name, total, done, done
	t.started = t.clock()
	t.sampleT, t.sampleB, t.speed = t.started, done, 0
	t.drawn = false
	if done > 0 {
		fmt.Fprintf(t.W, "Resuming %s at %s\n\n", name, FormatBytes(done))
	} else {
		fmt.Fprintf(t.W, "Downloading %s\n\n", name)
	}
	t.draw(true)
}

func (t *TerminalProgress) Update(done int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.done = done
	now := t.clock()
	if dt := now.Sub(t.sampleT).Seconds(); dt >= 0.5 {
		inst := float64(done-t.sampleB) / dt
		if t.speed == 0 {
			t.speed = inst
		} else {
			t.speed = 0.7*t.speed + 0.3*inst
		}
		t.sampleT, t.sampleB = now, done
	}
	interval := 100 * time.Millisecond
	if !t.TTY {
		interval = 5 * time.Second
	}
	if now.Sub(t.lastDraw) >= interval {
		t.draw(false)
	}
}

func (t *TerminalProgress) Finish(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if err == nil && t.total > 0 {
		t.done = t.total
	}
	t.draw(true)
	fmt.Fprintln(t.W)
}

// Retry reports a transient failure before the next resume attempt.
func (t *TerminalProgress) Retry(err error, attempt, max int, wait time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Fprintf(t.W, "Connection problem: %v\nRetrying in %s (attempt %d/%d), resuming from %s...\n\n",
		err, wait.Round(time.Second), attempt, max, FormatBytes(t.done))
}

// Lines returns the current three progress lines.
func (t *TerminalProgress) Lines() [3]string {
	var l [3]string
	const width = 38
	if t.total > 0 {
		pct := float64(t.done) / float64(t.total)
		if pct > 1 {
			pct = 1
		}
		fill := int(pct * width)
		l[0] = fmt.Sprintf("%s / %s", FormatBytes(t.done), FormatBytes(t.total))
		l[1] = fmt.Sprintf("%s%s %d%%", strings.Repeat("█", fill), strings.Repeat("░", width-fill), int(pct*100))
	} else {
		l[0] = FormatBytes(t.done)
		l[1] = strings.Repeat("░", width)
	}
	speed := t.speed
	if speed == 0 {
		if el := t.clock().Sub(t.started).Seconds(); el > 0 {
			speed = float64(t.done-t.startDone) / el
		}
	}
	l[2] = fmt.Sprintf("%s/s", FormatBytes(int64(speed)))
	if t.total > 0 && speed > 0 && t.done < t.total {
		eta := time.Duration(float64(t.total-t.done) / speed * float64(time.Second))
		l[2] += "   ETA " + formatETA(eta)
	}
	return l
}

func (t *TerminalProgress) draw(force bool) {
	t.lastDraw = t.clock()
	l := t.Lines()
	if !t.TTY {
		if force || t.drawn {
			fmt.Fprintf(t.W, "%s  %s  %s\n", l[0], strings.TrimSpace(l[1][strings.LastIndex(l[1], " "):]), l[2])
		}
		t.drawn = true
		return
	}
	if t.drawn {
		fmt.Fprint(t.W, "\x1b[3A")
	}
	for _, s := range l {
		fmt.Fprintf(t.W, "\r\x1b[2K%s\n", s)
	}
	t.drawn = true
}

// FormatBytes formats n with binary units.
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	units := []string{"KiB", "MiB", "GiB", "TiB"}
	v := float64(n) / unit
	i := 0
	for v >= unit && i < len(units)-1 {
		v /= unit
		i++
	}
	if v >= 100 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.2f %s", v, units[i])
}

func formatETA(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}
