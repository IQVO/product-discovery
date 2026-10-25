package ports

import "errors"

// ErrNotFound is what every repository returns for a Get that matches nothing
// (problem type not-found, 404). An adapter wraps its own miss (no rows, a
// missing map key) into this sentinel.
var ErrNotFound = errors.New("not found")

// ErrConcurrentUpdate is what a unit of work returns when a concurrent writer
// made it fail in a way a rerun fixes (a serialization failure or deadlock).
// The consumer reruns the whole unit.
var ErrConcurrentUpdate = errors.New("concurrent update")
