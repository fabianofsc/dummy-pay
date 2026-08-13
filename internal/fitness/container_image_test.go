package fitness

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestContainerImagePublication_IsMultiArchitecture(t *testing.T) {
	root := moduleRoot()
	workflow := readFile(t, filepath.Join(root, ".github", "workflows", "ci.yml"))

	command := exec.Command("make", "-n", "docker-push")
	command.Dir = root
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))
	publishCommand := string(output)

	for _, want := range []string{
		"docker buildx build",
		"--platform linux/amd64,linux/arm64",
		"--push",
	} {
		if !strings.Contains(publishCommand, want) {
			t.Errorf("docker-push command missing %q", want)
		}
	}

	publishImage := jobDefinition(workflow, "publish-image")
	if publishImage == "" {
		t.Fatal("workflow missing publish-image job")
	}
	steps := []string{
		"uses: actions/checkout@v4",
		"uses: docker/setup-qemu-action@v4",
		"uses: docker/setup-buildx-action@v4",
		"uses: docker/login-action@v4",
		"run: make docker-push",
	}
	previous := -1
	for _, step := range steps {
		position := strings.Index(publishImage, step)
		if position == -1 {
			t.Errorf("publish-image workflow missing %q", step)
			continue
		}
		if position <= previous {
			t.Errorf("publish-image workflow runs %q out of order", step)
		}
		previous = position
	}
}

func TestLintRunsEntireFitnessPackage(t *testing.T) {
	command := exec.Command("make", "-n", "lint")
	command.Dir = moduleRoot()
	output, err := command.CombinedOutput()
	require.NoError(t, err, string(output))

	if !strings.Contains(string(output), "go test ./internal/fitness/ -count=1") {
		t.Errorf("lint must run the entire internal/fitness package, got:\n%s", output)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(contents)
}

func jobDefinition(workflow, name string) string {
	marker := "\n  " + name + ":"
	start := strings.Index(workflow, marker)
	if start == -1 {
		return ""
	}

	job := workflow[start+len(marker):]
	nextJobPattern := regexp.MustCompile(`(?m)^  [^ ]`)
	if nextJob := nextJobPattern.FindStringIndex(job); nextJob != nil {
		return job[:nextJob[0]]
	}
	return job
}
