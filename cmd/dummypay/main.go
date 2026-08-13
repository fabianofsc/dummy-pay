// Command dummypay runs the DummyPay HTTP service. This is the only place
// concrete adapters are constructed and wired together (ADR-0003).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"dummypay/internal/clock"
	"dummypay/internal/config"
	httpapi "dummypay/internal/http"
	"dummypay/internal/payment"
	"dummypay/internal/postgres"
	"dummypay/internal/webhook"
)

func main() {
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	ctx := context.Background()

	if err := applyMigrations(ctx, cfg.DatabaseURL); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("connect to database: %v", err)
	}
	defer pool.Close()

	now := clock.Real{}
	accountID, err := postgres.SeedAccount(ctx, pool, clock.UUIDv7Generator{}.NewID(), cfg.AccountKeyID, now.Now())
	if err != nil {
		log.Fatalf("seed account: %v", err)
	}

	a := buildAdapters(cfg, pool)

	router := buildRouter(a, cfg, pool, accountID)
	worker := buildWorker(a, cfg, pool)

	stopTicker := startWorkerTicker(ctx, worker, cfg.WorkerPollInterval)
	defer stopTicker()

	log.Printf("dummypay listening on %s", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, router); err != nil {
		log.Fatal(err)
	}
}

// applyMigrations runs every embedded migration against the configured
// database, before the pool used for everything else is opened (ADR-0011).
func applyMigrations(ctx context.Context, databaseURL string) error {
	sqlDB, err := postgres.OpenForMigration(databaseURL)
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	return postgres.Migrate(ctx, sqlDB)
}

// adapters bundles the Postgres and clock adapters the router and the
// worker both depend on, built once in buildAdapters rather than separately
// in buildRouter and buildWorker — the two used to reconstruct the same
// PaymentRepository, SubscriptionRepository, DeliveryRepository, and
// OutboxWriter independently, risking the two call sites drifting apart.
type adapters struct {
	ids           clock.UUIDv7Generator
	realClock     clock.Real
	tx            *postgres.TxManager
	payments      *postgres.PaymentRepository
	subscriptions *postgres.SubscriptionRepository
	deliveries    *postgres.DeliveryRepository
	outbox        *postgres.OutboxWriter
}

// standardLogger adapts the process logger to payment.Logger. It is wired
// only here so internal/payment stays independent from infrastructure.
type standardLogger struct{}

func (standardLogger) Printf(format string, args ...any) {
	log.Printf(format, args...)
}

// buildAdapters constructs the adapters shared between buildRouter and
// buildWorker (spec §8, ADR-0003).
func buildAdapters(cfg config.Config, pool *pgxpool.Pool) adapters {
	ids := clock.UUIDv7Generator{}
	realClock := clock.Real{}
	return adapters{
		ids:           ids,
		realClock:     realClock,
		tx:            postgres.NewTxManager(pool),
		payments:      postgres.NewPaymentRepository(pool),
		subscriptions: postgres.NewSubscriptionRepository(pool, cfg.WebhookSecretEncKey),
		deliveries:    postgres.NewDeliveryRepository(pool, ids),
		outbox:        postgres.NewOutboxWriter(pool, ids, realClock),
	}
}

// buildRouter wires a into the three use cases the router serves, plus the
// idempotency store the router alone needs (spec §8, ADR-0003).
func buildRouter(a adapters, cfg config.Config, pool *pgxpool.Pool, accountID uuid.UUID) http.Handler {
	idempotency := postgres.NewIdempotencyStore(pool)

	createPayment := payment.NewCreatePaymentUseCase(payment.CreatePaymentDeps{
		Tx:               a.tx,
		Payments:         a.payments,
		Idempotency:      idempotency,
		Outbox:           a.outbox,
		Subscriptions:    a.subscriptions,
		Deliveries:       a.deliveries,
		Clock:            a.realClock,
		IDs:              a.ids,
		ProcessingDelay:  cfg.ProcessingDelay,
		IdempotencyLease: cfg.IdempotencyLease,
	})

	createSubscription := payment.NewCreateSubscriptionUseCase(payment.CreateSubscriptionDeps{
		Subscriptions: a.subscriptions,
		Secrets:       webhook.SecretGenerator{},
		IDs:           a.ids,
		Clock:         a.realClock,
	})

	retryDelivery := payment.NewRetryDeliveryUseCase(payment.RetryDeliveryDeps{
		Tx:         a.tx,
		Deliveries: a.deliveries,
		Outbox:     a.outbox,
		Clock:      a.realClock,
	})

	return httpapi.NewProductionRouter(httpapi.ProductionRouterDeps{
		Auth: httpapi.AuthConfig{
			AccountKeyID:     cfg.AccountKeyID,
			AccountKeySecret: cfg.AccountKeySecret,
		},
		AccountID:          accountID,
		Clock:              a.realClock,
		CreatePayment:      createPayment,
		CreateSubscription: createSubscription,
		RetryDelivery:      retryDelivery,
	})
}

// buildWorker wires a into the Worker, plus the outbox claimer and outbound
// sender it alone needs.
func buildWorker(a adapters, cfg config.Config, pool *pgxpool.Pool) *payment.Worker {
	return payment.NewWorker(payment.WorkerDeps{
		Tx:            a.tx,
		Claimer:       postgres.NewOutboxClaimer(pool),
		Payments:      a.payments,
		Subscriptions: a.subscriptions,
		Deliveries:    a.deliveries,
		Outbox:        a.outbox,
		Sender:        webhook.NewHTTPSender(cfg.WebhookTimeout),
		Logger:        standardLogger{},
		IDs:           a.ids,
		Clock:         a.realClock,
	})
}

// startWorkerTicker wraps Worker.ProcessBatch in a ticker at interval,
// stopping when the returned func is called. The worker itself is a plain
// method that processes one batch and returns (ADR-0012) — this ticker is
// the only place in the service, besides internal/clock, driven by real
// wall-clock time.
func startWorkerTicker(ctx context.Context, w *payment.Worker, interval time.Duration) (stop func()) {
	const batchSize = 50

	ticker := time.NewTicker(interval)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ticker.C:
				if err := w.ProcessBatch(ctx, batchSize); err != nil {
					log.Printf("worker batch error: %v", err)
				}
			case <-done:
				return
			}
		}
	}()

	return func() {
		ticker.Stop()
		close(done)
	}
}
