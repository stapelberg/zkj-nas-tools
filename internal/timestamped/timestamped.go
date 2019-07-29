// Package timestamped provides types which record when their value last
// changed. This is useful for implementing hysteresis.
package timestamped

import "time"

// Bool wraps the builtin bool type.
type Bool struct {
	lastChange time.Time
	value      bool
}

// Set updates the value, if val differs from the current value.
func (b *Bool) Set(val bool) {
	if b.value == val && !b.lastChange.IsZero() {
		return
	}
	b.lastChange = time.Now()
	b.value = val
}

// Value returns the current value.
func (b *Bool) Value() bool {
	return b.value
}

// LastChange returns when Set last changed a value.
func (b *Bool) LastChange() time.Time {
	return b.lastChange
}
