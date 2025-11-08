//go:build race

package executor

// raceEnabled is true when compiled with -race flag
func init() {
	raceEnabled = true
}
