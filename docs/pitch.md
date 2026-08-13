# DummyPay — Apresentação

> Um provedor de pagamentos com cartão que não é real, de propósito.

## O problema

Toda equipe que recebe pagamentos com cartão precisa escrever o mesmo código:
montar uma requisição de cobrança, lidar com uma aprovação, lidar com uma
recusa, sobreviver a um timeout, reconciliar um webhook que chega atrasado e
nunca cobrar o cliente duas vezes. Esse código está entre os de maior impacto
no produto.

Também está entre os mais difíceis de testar.

As opções usuais sempre deixam alguma lacuna:

- **Sandboxes de provedores** são compartilhados, remotos e ocasionalmente ficam
  indisponíveis. Uma suíte que depende deles falha por motivos que não têm nada
  a ver com sua mudança. A CI precisa de acesso à rede e de credenciais reais
  armazenadas.
- **Clientes HTTP simulados** testam se você consegue interpretar uma resposta
  que você mesmo escreveu. Eles não exercitam chaves de idempotência, retries,
  requisições concorrentes para o mesmo pedido ou um webhook que chega antes de
  a transação do seu banco ser confirmada.
- **O plano de “testar em staging”** faz com que o primeiro exercício real do
  caminho de recusa aconteça quando um cliente o aciona.

O resultado é previsível. Aprovação e recusa são testadas. Todo o restante — o
caminho assíncrono, a requisição duplicada, o webhook que falhou e precisa ser
reenviado — é descoberto em produção.

## O que é o DummyPay

O DummyPay é um provedor de serviços de pagamento que você executa por conta
própria. Ele expõe uma API HTTP pequena e transparente, mantém seu próprio
banco PostgreSQL e se comporta como um PSP real exatamente nas formas que
tornam o código de integração difícil de acertar.

Você aponta seu checkout para ele, em vez de um provedor real, e obtém:

**Cenários determinísticos em vez de cartões de teste.** Você não envia um
número de cartão, porque o DummyPay nunca quer recebê-lo. Você envia um token
de cenário opaco — `card_approved`, `card_declined`,
`card_processing_approved`, `card_processing_declined` — e o resultado é
exatamente o que o nome informa, sempre. Sem tabelas de cartões de teste, sem
“este número deixou de funcionar no trimestre passado”.

**Um caminho assíncrono real.** Os quatro cenários retornam `PROCESSING`
primeiro e liquidam depois. Esse é o caminho que mais costuma dar errado nas
integrações, e geralmente o mais difícil de disparar sob demanda. Aqui ele é
controlado por um token. O atraso de liquidação é configurado por ambiente:
alguns segundos localmente para que você possa observá-lo, zero nos testes para
que a suíte permaneça rápida e determinística.

**Idempotência e concorrência que realmente se impõem.** Repetir uma
`Idempotency-Key` devolve a transação original. Reutilizar uma chave com corpo
diferente não cria silenciosamente uma segunda cobrança. Enviar duas requisições
concorrentes com a mesma chave enquanto a primeira ainda está em andamento
retorna `409`, não uma corrida. Essa é a semântica que provedores reais impõem,
e a única maneira de saber se seu cliente a trata corretamente é executá-lo
contra algo que a faça cumprir.

**Webhooks que você pode quebrar e reparar.** As assinaturas são registradas
antecipadamente, não vinculadas a pagamentos individuais — como provedores reais
fazem. Toda entrega é persistida *antes* da tentativa, carrega uma assinatura
HMAC-SHA-256 sobre o corpo bruto e registra seu próprio status, contagem de
tentativas, horário da última tentativa e último status HTTP. Quando seu
consumidor está indisponível, a entrega fica `FAILED` em uma tabela onde você
pode vê-la. Quando ele volta, você a reenvia com uma chamada. A verificação de
assinatura é algo para o qual agora você pode escrever um teste real.

A ordem de entrega não é garantida, deliberadamente. Provedores reais também
não a garantem, e um consumidor que depende silenciosamente de ordenação é
exatamente o tipo de erro que vale a pena encontrar no notebook, em vez de em
produção.

**Nada sensível, nunca.** O DummyPay não aceita PAN, CVV ou data de expiração.
Não há campo onde colocá-los. Ele não pode vazar dados de portador porque nunca
os possui — portanto pode ser executado em um notebook, na CI, em um ambiente
de desenvolvimento compartilhado ou em uma demonstração, sem uma conversa sobre
conformidade.

## O que ele não é

O DummyPay não é um processador de pagamentos, nem finge ser um. Ele não move
dinheiro nem toca uma rede de cartões. Não é um sistema compatível com PCI,
porque deliberadamente se coloca fora do escopo descrito pela PCI.

Também é intencionalmente pequeno. Não há tokenização nem cofre de cartões. Não
há auth-only, pré-autorização ou captura posterior/parcial. Não há reembolsos,
chargebacks, 3DS, parcelamentos, antifraude, split payments, assinaturas, Pix ou
boleto. Não há multitenancy, dashboard ou API pública. Uma conta técnica por
ambiente, uma assinatura de webhook ativa, uma operação: vender.

Cada uma dessas ausências é uma decisão, não uma lacuna. O valor deste projeto
é que sua superfície permaneça pequena o bastante para inspirar confiança.

## Para quem é

Equipes de backend que estão construindo ou mantendo um checkout e querem sua
integração de pagamentos coberta por testes que executam offline e passam pelas
razões certas.

## Como é construído

Go, com seu próprio PostgreSQL e nenhuma outra dependência. Três endpoints por
trás de `chi`, um pacote de domínio que não importa infraestrutura e trabalho
assíncrono — tanto a liquidação atrasada quanto a entrega de webhook — em uma
tabela outbox escrita na mesma transação que produziu a mudança. Nada é enviado
sem antes ter sido registrado.

As duas propriedades às quais todo o resto serve: **a semântica sob teste é a
semântica entregue** — idempotência e a reivindicação de trabalho do worker são
comprovadas em PostgreSQL real, nunca em um fake que concordaria com qualquer
coisa que escrevêssemos — e **nenhum teste espera pelo relógio**, porque o tempo
é injetado e a agenda é dado.

Cada escolha, inclusive as que foram próximas e seus custos, está registrada em
[decisions/](decisions/).

## Como é “pronto” na V1

O serviço inicia e a suíte completa de testes passa sem rede externa. A suíte
cobre aprovação, recusa, os cenários assíncronos, replay idempotente,
requisições duplicadas concorrentes, geração da assinatura HMAC, falha na
entrega do webhook e replay bem-sucedido. Uma pessoa desenvolvedora pode clonar
o repositório e executá-lo localmente seguindo o README.

---

Consulte o [README](../README.md) para o contrato da API e a configuração local,
e [decisions/](decisions/) para os registros de decisões arquiteturais.
