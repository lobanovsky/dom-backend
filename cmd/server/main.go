package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"dom-backend/internal/auth"
	"dom-backend/internal/config"
	"dom-backend/internal/db"
	"dom-backend/internal/httpapi"
	"dom-backend/internal/logx"
	"dom-backend/internal/store"
)

func main() {
	// LOG_FILE (необязательно): журнал ещё и в файл с ротацией; читается до загрузки конфига, чтобы ошибки конфига тоже попали в журнал.
	log, closeLog := logx.New(os.Getenv("LOG_FILE"))
	slog.SetDefault(log)
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		closeLog()
		os.Exit(1)
	}
	closeLog()
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := db.Migrate(cfg.DatabaseURL); err != nil {
		return err
	}
	pool, err := db.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	a := auth.New(cfg.AdminUsername, cfg.AdminPasswordHash, cfg.AdminSessionSecret, cfg.SessionTTL)

	premises := store.NewPremisesStore(pool)
	ownerships := store.NewOwnerships(pool)
	accounts := store.NewAccounts(pool)
	paymentRules := store.NewPaymentRules(pool)

	srv := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: httpapi.NewRouter(httpapi.Deps{
			Auth:               a,
			Organizations:      store.NewOrganizations(pool),
			Buildings:          store.NewBuildings(pool),
			Premises:           premises,
			Persons:            store.NewPersons(pool),
			LegalEntities:      store.NewLegalEntities(pool),
			Ownerships:         ownerships,
			Residencies:        store.NewResidencies(pool),
			Accounts:           accounts,
			Holders:            store.NewAccountHolders(pool),
			Importer:           store.NewImporter(pool),
			Properties:         store.NewProperties(pool),
			PaymentRegistries:  store.NewPaymentRegistries(pool),
			BankStatements:     store.NewBankStatements(pool),
			PaymentRules:       paymentRules,
			RuleOrder:          paymentRules,
			Assignments:        store.NewAssignments(pool),
			BankAccounts:       store.NewBankAccounts(pool),
			PaymentCategories:  store.NewPaymentCategories(pool),
			IncomingPayments:   store.NewIncomingPayments(pool),
			OutgoingPayments:   store.NewOutgoingPayments(pool),
			PremisesOwnerships: ownerships,
			PremisesAccounts:   accounts,
		}),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.ListenAddr)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
