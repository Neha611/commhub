//go:build unix

package main

import "golang.org/x/sys/unix"

// hardenProcess disables core dumps. Go strings are immutable and cannot be
// reliably zeroed, so credentials linger in the heap until GC; a core file
// would put them on disk in plaintext (SEC-12).
func hardenProcess() {
	_ = unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{Cur: 0, Max: 0})
}
