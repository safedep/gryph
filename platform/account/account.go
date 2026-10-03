// Package account identifies the operating-system account of the current
// process. The identifier is stable across runs and unique on the host, so
// Gryph can derive per-account state from it, such as the system session
// that holds tamper events. Each platform supplies its own form: the numeric
// uid on Unix, the SID on Windows. Callers treat it as an opaque string.
package account

// CurrentID returns the identifier of the account that runs this process.
func CurrentID() (string, error) {
	return currentID()
}
