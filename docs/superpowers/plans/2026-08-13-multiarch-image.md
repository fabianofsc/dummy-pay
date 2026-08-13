# Publicação multi-arquitetura da imagem Docker — Plano de implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publicar `fabianofsc/dummy-pay:latest` com variantes nativas
`linux/amd64` e `linux/arm64` para que o Docker escolha a correta em macOS,
Linux e Windows com Docker Desktop.

**Architecture:** O job existente `publish-image` prepara emulação QEMU e um
builder Buildx antes de autenticar e chamar o alvo de publicação. O alvo
`docker-push` cria e envia em uma operação o manifest index que referencia os
dois manifests de plataforma. Um teste de fitness verifica estaticamente esse
contrato, sem depender de Docker ou da rede.

**Tech Stack:** Docker Buildx, QEMU, GitHub Actions, Make, Go `testing`.

## Global Constraints

- Publicar somente em `push` para `main`, depois do job `test`.
- Publicar exclusivamente `linux/amd64,linux/arm64`; não usar `platform:` nos
  consumidores nem criar imagem Windows nativa.
- Não adicionar dependências Go nem serviços externos.
- O Dockerfile deve permanecer sem plataforma fixada.
- Não incluir segredos no repositório; manter as credenciais do ambiente
  `production`.
- Não criar commit ou push sem solicitação explícita do usuário.

---

### Task 1: Cobrir o contrato multi-arquitetura com teste de fitness

**Files:**

- Create: `internal/fitness/container_image_test.go`
- Test: `internal/fitness/container_image_test.go`

**Consumes:** `Makefile` e `.github/workflows/ci.yml` na raiz do módulo.

**Produces:** `TestContainerImagePublication_IsMultiArchitecture`, que executa
`make -n docker-push` e falha quando o comando gerado não pede ambas as
plataformas; também falha quando o workflow não prepara QEMU e Buildx.

- [x] **Step 1: Escrever o teste que falha**

```go
func TestContainerImagePublication_IsMultiArchitecture(t *testing.T) {
	root := moduleRoot()
	workflow := readFile(t, filepath.Join(root, ".github", "workflows", "ci.yml"))

	cmd := exec.Command("make", "-n", "docker-push")
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
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
	for _, want := range []string{
		"docker/setup-qemu-action@v4",
		"docker/setup-buildx-action@v4",
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("publish-image workflow missing %q", want)
		}
	}
}
```

The helper `readFile(t, path)` must call `os.ReadFile`, call `t.Helper()`, and
fail the test with `t.Fatalf` if it cannot read the file.

- [x] **Step 2: Executar o teste e observar a falha esperada**

Run: `go test ./internal/fitness -run TestContainerImagePublication_IsMultiArchitecture -count=1`

Expected: FAIL, reporting the missing `docker buildx build`, platform list,
`--push`, QEMU, and Buildx setup.

- [x] **Step 3: Confirmar que a falha protege a mudança**

The Makefile assertion observes the real command that `docker-push` would run,
without calling Docker. It specifically fails against the current `docker
build` plus separate `docker push` implementation. The workflow configuration
is a repository fitness function and is inspected statically, as are the
existing architecture and time-discipline checks.

### Task 2: Publicar o manifest index com Buildx

**Files:**

- Modify: `Makefile:1-39`
- Modify: `.github/workflows/ci.yml:65-81`
- Test: `internal/fitness/container_image_test.go`

**Consumes:** the failing fitness test and the existing Docker Hub login.

**Produces:** a `docker-push` target that sends a `linux/amd64` +
`linux/arm64` manifest index, and a `publish-image` job able to build it.

- [x] **Step 1: Alterar o alvo de publicação de forma mínima**

Replace the two commands in `docker-push` with this single command:

```make
docker buildx build --platform linux/amd64,linux/arm64 --push -t $(DOCKER_IMAGE):latest .
```

Keep `DOCKER_IMAGE ?= fabianofsc/dummy-pay` and all local-development targets
unchanged.

- [x] **Step 2: Preparar o runner antes do login**

Insert these steps immediately after checkout in `publish-image`:

```yaml
      - name: Set up QEMU
        uses: docker/setup-qemu-action@v4

      - name: Set up Docker Buildx
        uses: docker/setup-buildx-action@v4
```

Keep the login and `make docker-push` steps after them, and change no event
conditions, permissions, or secrets.

- [x] **Step 3: Executar o teste de fitness e observar sucesso**

Run: `go test ./internal/fitness -run TestContainerImagePublication_IsMultiArchitecture -count=1`

Expected: PASS.

### Task 3: Verificar integração e higiene da alteração

**Files:**

- Verify: `Makefile`, `.github/workflows/ci.yml`, and
  `internal/fitness/container_image_test.go`

**Consumes:** completed Buildx publication configuration.

**Produces:** evidence that the project and the static publication contract
remain healthy before the next push activates the workflow.

- [x] **Step 1: Validar a sintaxe YAML com Python**

Run: `python3 -c 'import yaml; yaml.safe_load(open(".github/workflows/ci.yml")); print("valid YAML")'`

Expected: `valid YAML` and exit code `0`.

- [x] **Step 2: Executar verificações locais completas**

Run: `make lint && go test ./...`

Expected: both commands exit `0`.

- [x] **Step 3: Revisar o diff final**

Run: `git diff --check && git diff -- Makefile .github/workflows/ci.yml internal/fitness/container_image_test.go`

Expected: no whitespace errors; only Buildx/QEMU setup, multi-architecture
publication, and its static regression test appear.

- [x] **Step 4: State the deployment limitation clearly**

The local checks prove the repository configuration only. The Docker Hub
manifest can be inspected with `docker buildx imagetools inspect
fabianofsc/dummy-pay:latest` after the next successful push to `main`; do not
claim remote publication before that workflow has run.
