// Package clock hides the current time so that the demo can move it forward
// and tests can fix it. The scheduler (module of Бэк-1) uses the same clock.
package clock

import (
	"sync"
	"time"
)

type Clock interface {
	Now() time.Time
}

type System struct{}

func (System) Now() time.Time { return time.Now() }

// Manual is a settable clock for tests and the demo mode.
type Manual struct {
	mu sync.Mutex
	t  time.Time
}

func NewManual(t time.Time) *Manual { return &Manual{t: t} }

func (m *Manual) Now() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.t
}

func (m *Manual) Set(t time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.t = t
}

// Today returns the calendar date (YYYY-MM-DD) of c in loc.
func Today(c Clock, loc *time.Location) string {
	return c.Now().In(loc).Format(time.DateOnly)
}
