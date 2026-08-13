# Docker Hub Publish Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build and push DummyPay to Docker Hub through one Make target.

**Architecture:** Define a default Docker Hub image repository. `docker-push`
will build and push only the fixed `latest` tag; callers can override the image
repository.

**Tech Stack:** GNU Make, Docker CLI, existing Dockerfile.

## Global Constraints

- Never store Docker Hub credentials or configuration in the repository.
- The target must use `fabianofsc/dummy-pay` by default.
- The target must push only the fixed `latest` tag by default.
- Do not publish during dry-run verification; publish only because the user explicitly requested it.

---

### Task 1: Add and verify Docker Hub publishing target

**Files:**
- Modify: `Makefile`
- Test: `make -n docker-push`

**Consumes:** an existing Docker login and `Dockerfile`.

**Produces:** `make docker-push [DOCKER_IMAGE=<repo>]`.

- [x] **Step 1: Verify the target is absent**

Run: `make -n docker-push`

Expected: FAIL with `No rule to make target 'docker-push'`.

- [x] **Step 2: Add the Make variables and target**

```make
DOCKER_IMAGE ?= fabianofsc/dummy-pay

docker-push:
	docker build -t $(DOCKER_IMAGE):latest .
	docker push $(DOCKER_IMAGE):latest
```

- [x] **Step 3: Verify the generated commands without publishing**

Run: `make -n docker-push`

Expected: one build command with the fixed tag, followed by one push.

- [x] **Step 4: Publish the fixed tag using the authenticated Docker session**

Run: `make docker-push`

Expected: Docker Hub reports a digest for `latest`.
