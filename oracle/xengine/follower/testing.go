package follower

// RegisterForTest adds f to the registry until the returned restore is called,
// so a test in another package can drive the harness with a follower whose
// runner is a test script (a follower-export follower before any real one
// exists). An entry already named f.Name is shadowed, not replaced. Not safe
// for parallel tests; never call it outside a test.
func RegisterForTest(f Follower) (restore func()) {
	saved := registry
	registry = append([]Follower{f}, saved...)
	return func() { registry = saved }
}
