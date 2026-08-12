package fitness

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var allowedTimePackages = map[string]bool{
	"internal/clock":   true,
	"cmd":              true,
	"internal/fitness": true,
}

var timeCallPatterns = []struct {
	pattern string
	name    string
}{
	{`time\.Now\(`, "time.Now()"},
	{`time\.Sleep\(`, "time.Sleep()"},
	{`time\.After\(`, "time.After()"},
	{`time\.AfterFunc\(`, "time.AfterFunc()"},
	{`time\.NewTicker\(`, "time.NewTicker()"},
}

func TestTimeDiscipline_NoRawTimeCallsOutsideClockAndCmd(t *testing.T) {
	root := moduleRoot()
	var violations []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		for _, skip := range []string{"vendor", ".git", ".claude"} {
			if strings.Contains(path, skip) {
				return nil
			}
		}

		allowed := false
		for prefix := range allowedTimePackages {
			if strings.Contains(path, prefix) {
				allowed = true
				break
			}
		}
		if allowed {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		scanner := bufio.NewScanner(file)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "//") {
				continue
			}
			for _, p := range timeCallPatterns {
				if matched, _ := regexp.MatchString(p.pattern, line); matched {
					violations = append(violations, path+":"+itoa(lineNum)+" "+p.name)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk files: %v", err)
	}

	for _, v := range violations {
		t.Errorf("disallowed time call at %s (ADR-0012: time calls only in internal/clock and cmd/)", v)
	}
}
