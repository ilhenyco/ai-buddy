package clock

import (
	"sync"
	"time"
)

// Offset shifts another clock. The demo uses it to "move to next week" without
// touching the real time (sessions keep using the real clock).
type Offset struct {
	base Clock
	mu   sync.Mutex
	d    time.Duration
}

func NewOffset(base Clock) *Offset { return &Offset{base: base} }

func (o *Offset) Now() time.Time {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.base.Now().Add(o.d)
}

// SetToday moves the clock so that today in loc is date (YYYY-MM-DD), keeping the time of day.
func (o *Offset) SetToday(date string, loc *time.Location) error {
	day, err := time.ParseInLocation(time.DateOnly, date, loc)
	if err != nil {
		return err
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	now := o.base.Now().In(loc)
	target := time.Date(day.Year(), day.Month(), day.Day(), now.Hour(), now.Minute(), now.Second(), now.Nanosecond(), loc)
	o.d = target.Sub(now)
	return nil
}

// Reset returns to the base clock.
func (o *Offset) Reset() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.d = 0
}

// Shifted reports whether the clock is moved away from the base clock.
func (o *Offset) Shifted() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.d != 0
}
