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

	router := buildRouter(cfg, pool, accountID)
	worker := buildWorker(cfg, pool)

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

// buildRouter constructs every adapter the HTTP layer needs and wires them
// into the three use cases the router serves (spec §8, ADR-0003).
func buildRouter(cfg config.Config, pool *pgxpool.Pool, accountID uuid.UUID) http.Handler {
	ids := clock.UUIDv7Generator{}
	realClock := clock.Real{}
	tx := postgres.NewTxManager(pool)
	payments := postgres.NewPaymentRepository(pool)
	idempotency := postgres.NewIdempotencyStore(pool)
	outbox := postgres.NewOutboxWriter(pool, ids, realClock)
	subscriptions := postgres.NewSubscriptionRepository(pool, cfg.WebhookSecretEncKey)
	deliveries := postgres.NewDeliveryRepository(pool, ids)

	createPayment := payment.NewCreatePaymentUseCase(payment.CreatePaymentDeps{
		Tx:               tx,
		Payments:         payments,
		Idempotency:      idempotency,
		Outbox:           outbox,
		Subscriptions:    subscriptions,
		Deliveries:       deliveries,
		Clock:            realClock,
		IDs:              ids,
		ProcessingDelay:  cfg.ProcessingDelay,
		IdempotencyLease: cfg.IdempotencyLease,
	})

	createSubscription := payment.NewCreateSubscriptionUseCase(payment.CreateSubscriptionDeps{
		Subscriptions: subscriptions,
		Secrets:       webhook.SecretGenerator{},
		IDs:           ids,
		Clock:         realClock,
	})

	retryDelivery := payment.NewRetryDeliveryUseCase(payment.RetryDeliveryDeps{
		Tx:         tx,
		Deliveries: deliveries,
		Outbox:     outbox,
		Clock:      realClock,
	})

	return httpapi.NewProductionRouter(httpapi.ProductionRouterDeps{
		Auth: httpapi.AuthConfig{
			AccountKeyID:     cfg.AccountKeyID,
			AccountKeySecret: cfg.AccountKeySecret,
		},
		AccountID:          accountID,
		CreatePayment:      createPayment,
		CreateSubscription: createSubscription,
		RetryDelivery:      retryDelivery,
	})
}

// buildWorker constructs the Worker with the same adapters the router uses,
// plus the outbox claimer and outbound sender it alone needs.
func buildWorker(cfg config.Config, pool *pgxpool.Pool) *payment.Worker {
	ids := clock.UUIDv7Generator{}
	realClock := clock.Real{}

	return payment.NewWorker(payment.WorkerDeps{
		Tx:            postgres.NewTxManager(pool),
		Claimer:       postgres.NewOutboxClaimer(pool),
		Payments:      postgres.NewPaymentRepository(pool),
		Subscriptions: postgres.NewSubscriptionRepository(pool, cfg.WebhookSecretEncKey),
		Deliveries:    postgres.NewDeliveryRepository(pool, ids),
		Outbox:        postgres.NewOutboxWriter(pool, ids, realClock),
		Sender:        webhook.NewHTTPSender(cfg.WebhookTimeout),
		IDs:           ids,
		Clock:         realClock,
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
