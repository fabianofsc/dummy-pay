# Publicação multi-arquitetura da imagem Docker

## Objetivo

Publicar `fabianofsc/dummy-pay:latest` como uma única imagem OCI compatível
com `linux/amd64` e `linux/arm64`, para que Docker Compose escolha a variante
nativa no macOS, Linux e Windows com Docker Desktop.

## Contexto

O workflow publica a imagem a partir de um runner `ubuntu-latest`, que produz
somente `linux/amd64`. Em um host ARM, como um Mac Apple Silicon, o Docker
precisa emular a imagem e avisa que a plataforma solicitada não corresponde à
plataforma do host.

## Decisão

O job `publish-image` configurará QEMU e Docker Buildx (ações `@v4`) e fará um único build
com `linux/amd64,linux/arm64`, enviando o manifest index e ambas as imagens à
mesma tag `fabianofsc/dummy-pay:latest`.

O `Dockerfile` continua sem uma diretiva `platform`: os estágios recebem a
plataforma-alvo do Buildx e produzem um binário Go para ela. Os consumidores
continuam usando a tag normal, sem `platform:` no Compose.

## Alterações

- `Makefile`: o alvo de publicação usará `docker buildx build --platform
  linux/amd64,linux/arm64 --push`; ele não fará `docker push` separado, porque
  o Buildx publica o manifest ao final do build.
- `.github/workflows/ci.yml`: antes da publicação, instalar QEMU e criar o
  builder Buildx; manter login no Docker Hub e a condição de publicar apenas
  em push para `main`.
- Teste de regressão: uma verificação estática em Go confirmará que o alvo e o
  workflow declaram ambas as plataformas, Buildx e QEMU. Isso protege a
  configuração contra regressão sem depender de Docker Hub ou da rede.

## Critérios de aceite

- A tag publicada contém manifestos `linux/amd64` e `linux/arm64`.
- `docker compose up` com a imagem não emite o aviso de incompatibilidade em
  hosts ARM nem exige emulação.
- Um host x86 continua selecionando `linux/amd64` automaticamente.
- A suíte local verifica a configuração sem acesso à rede.

## Fora de escopo

- Imagens Windows nativas: Docker Desktop executa contêineres Linux no Windows,
  portanto usa a variante `linux/amd64` ou `linux/arm64` correspondente.
- Alterar o Compose consumidor ou fixar uma plataforma nele.
- Criar tags por arquitetura ou alterar a política de tags existente.
