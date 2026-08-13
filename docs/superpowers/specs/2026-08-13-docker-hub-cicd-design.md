# Docker Hub CI/CD Design

## Goal

Publish the DummyPay Docker image automatically whenever a commit is pushed to
`main`, but only after the existing build, formatting, vet, and database-backed
test checks succeed.

## Design

Extend `.github/workflows/ci.yml` with a `publish-image` job that depends on
the existing `test` job. Its job-level condition limits it to `push` events on
`refs/heads/main`, so pull requests execute validation only and never publish an
image.

The publishing job targets the GitHub environment `production`, checks out the
exact triggering revision, authenticates to Docker Hub with that environment's
secrets `DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`, then runs the repository's
existing `make docker-push` target. The target publishes the fixed
`fabianofsc/dummy-pay:latest` tag.

The job grants only `contents: read` permission. Docker Hub credentials are
never committed, printed, or passed through repository configuration; they are
used only by the Docker login action.

## Operator Setup

Before the first eligible push, configure these GitHub environment secrets in
`production`:

- `DOCKERHUB_USERNAME`: Docker Hub account name, `fabianofsc`.
- `DOCKERHUB_TOKEN`: Docker Hub access token with permission to push the
  `fabianofsc/dummy-pay` repository.

Without either secret, the publish job fails safely and no image is pushed.

## Scope

This change does not alter the application image, add registry credentials to
the repository, publish pull-request images, add image signing, or introduce
multi-architecture builds.
