# Docker Hub Publish Design

## Goal

Provide a safe, repeatable Make target that builds and publishes DummyPay to
the authenticated Docker Hub account as `fabianofsc/dummy-pay`.

## Design

The Makefile will expose one overridable variable:

- `DOCKER_IMAGE ?= fabianofsc/dummy-pay`

`make docker-push` will build the current checkout and publish it as
`$(DOCKER_IMAGE):latest`. The tag stays fixed so consumers always pull the
same image version reference.

The target relies on an existing `docker login`; it neither reads nor writes
Docker Hub credentials. Callers can override the image, for example:

```sh
make docker-push DOCKER_IMAGE=example/dummy-pay
```

## Error Handling and Verification

Docker itself stops the Make target if the daemon is unavailable, the user is
not authenticated, the build fails, or the push fails. A dry run with
`make -n docker-push` will verify the expected build tag and push command
without publishing anything.

## Scope

This adds local publishing only. It does not add Docker Hub credentials,
registry automation, CI publishing, image signing, or multi-architecture
builds.
