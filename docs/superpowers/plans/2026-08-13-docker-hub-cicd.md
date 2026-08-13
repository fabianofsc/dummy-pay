# Docker Hub CI/CD Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish the DummyPay Docker image to Docker Hub automatically after a
successful CI run caused by a push to `main`.

**Architecture:** Keep validation and publication in the existing workflow.
The `publish-image` job depends on `test`, references the GitHub `production`
environment to receive its secrets, and calls the existing `make docker-push`
target for the fixed `latest` tag.

**Tech Stack:** GitHub Actions, `docker/login-action@v4`, Docker Hub, Make.

## Global Constraints

- Publish only for `push` events on `main`; pull requests must never publish.
- The publishing job must run only after the `test` job succeeds.
- Use environment secrets `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` from
  `production`; never place their values in the repository.
- Set GitHub token permissions to `contents: read`.
- Preserve existing uncommitted Docker publishing, logging, and documentation
  changes.

---

### Task 1: Add the Docker Hub publishing job

**Files:**
- Modify: `.github/workflows/ci.yml`

**Consumes:** the existing `test` job, GitHub environment `production`, and the
`make docker-push` target.

**Produces:** `publish-image`, a post-test job that logs into Docker Hub and
publishes `fabianofsc/dummy-pay:latest`.

- [x] **Step 1: Add a failing structural check for the workflow contract**

Run the following command before editing the workflow. It must fail because the
`publish-image` job does not exist yet:

```sh
ruby -e 'workflow = File.read(".github/workflows/ci.yml"); abort "missing publish job" unless workflow.include?("publish-image:")'
```

- [x] **Step 2: Add the minimal publishing job**

Add the following workflow-level permission and job, retaining the current
`test` job unchanged:

```yaml
permissions:
  contents: read

jobs:
  # existing test job
  publish-image:
    needs: test
    if: github.event_name == 'push' && github.ref == 'refs/heads/main'
    environment: production
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Log in to Docker Hub
        uses: docker/login-action@v4
        with:
          username: ${{ secrets.DOCKERHUB_USERNAME }}
          password: ${{ secrets.DOCKERHUB_TOKEN }}

      - name: Publish image
        run: make docker-push
```

- [x] **Step 3: Run the structural check and YAML parser**

Run:

```sh
ruby -e 'workflow = File.read(".github/workflows/ci.yml"); %w[publish-image needs: test environment: production docker/login-action@v4 DOCKERHUB_USERNAME DOCKERHUB_TOKEN make\ docker-push].each { |text| abort "missing #{text}" unless workflow.include?(text) }'
ruby -e 'require "yaml"; YAML.load_file(".github/workflows/ci.yml")'
```

Expected: both commands exit `0`.

### Task 2: Verify the repository remains healthy

**Files:**
- Verify: `.github/workflows/ci.yml` and current worktree

**Consumes:** the completed CI/CD workflow.

**Produces:** evidence that the workflow file is syntactically valid and no
application behavior was regressed.

- [x] **Step 1: Run lint and the full Go suite**

Run:

```sh
make lint
go test ./...
```

Expected: both commands exit `0`.

- [x] **Step 2: Review diff hygiene**

Run:

```sh
git diff --check
git diff -- .github/workflows/ci.yml
```

Expected: no whitespace errors; the workflow only adds the post-test,
main-only publishing path.
