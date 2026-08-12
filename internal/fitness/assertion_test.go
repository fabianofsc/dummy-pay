package fitness

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var forbiddenAssertionImports = []string{
	`"github.com/stretchr/testify/assert"`,
	`"github.com/stretchr/testify/mock"`,
	`"github.com/stretchr/testify/suite"`,
}

// requireEqualOnTimed match require.Equal(t, ...) on types whose structs
// contain time.Time fields — the list grows with the domain model.
var requireEqualTimedPattern = regexp.MustCompile(
	`require\.Equal\(t,\s*\w*(Payment|Delivery|Subscription|IdempotencyRecord)\b`)

func moduleRoot() string {
	cmd := exec.Command("go", "env", "GOMOD")
	out, err := cmd.Output()
	if err != nil {
		return "."
	}
	modPath := strings.TrimSpace(string(out))
	return filepath.Dir(modPath)
}

func TestAssertionDiscipline_NoForbiddenTestifyImports(t *testing.T) {
	root := moduleRoot()
	var violations []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		for _, skip := range []string{"vendor", ".git", ".claude"} {
			if strings.Contains(path, skip) {
				return nil
			}
		}
		// Skip this package's own files
		if strings.Contains(path, "internal/fitness") {
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
			line := scanner.Text()
			for _, forbidden := range forbiddenAssertionImports {
				if strings.Contains(line, forbidden) {
					violations = append(violations, path+":"+itoa(lineNum)+" imports "+forbidden)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk files: %v", err)
	}

	for _, v := range violations {
		t.Errorf("forbidden testify import at %s (ADR-0014: never testify/assert, testify/mock, or testify/suite)", v)
	}
}

func TestAssertionDiscipline_NoRequireEqualOnTimedStructs(t *testing.T) {
	root := moduleRoot()
	var violations []string

	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		for _, skip := range []string{"vendor", ".git", ".claude", "internal/fitness"} {
			if strings.Contains(path, skip) {
				return nil
			}
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
			line := scanner.Text()
			if requireEqualTimedPattern.MatchString(line) {
				violations = append(violations, path+":"+itoa(lineNum))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk files: %v", err)
	}

	for _, v := range violations {
		t.Errorf("require.Equal on timestamped struct at %s (ADR-0014: use cmp.Diff)", v)
	}
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
