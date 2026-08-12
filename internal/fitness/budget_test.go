package fitness

import "testing"

// TestSuiteWallClockBudget documents the wall-clock budget the full suite
// (all packages, with database) must fit inside. The budget is enforced by
// the CI workflow and the Makefile lint target; this test exists so that the
// number is discoverable in code and a reader updating the test suite sees it
// here.
//
// Budget: 120 seconds with database, 30 seconds without.
// Measured on macOS: ~12s with -race -count=1, ~52s with -race -count=5.
func TestSuiteWallClockBudget(t *testing.T) {
	const dbBudget = "120s"
	const noDBBudget = "30s"

	// This test always passes. Actual enforcement is in Makefile and CI.
	t.Logf("suite budget with database: %s, without: %s", dbBudget, noDBBudget)
}
