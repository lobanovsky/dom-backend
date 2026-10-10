package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"dom-backend/internal/auth"
	"dom-backend/internal/config"
	"dom-backend/internal/db"
	"dom-backend/internal/httpapi"
	"dom-backend/internal/logx"
	"dom-backend/internal/sberapi"
	"dom-backend/internal/sbersync"
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

	sberStore := store.NewSber(pool)
	if err := sberStore.AbortUnfinished(ctx); err != nil {
		return err
	}
	bankStatements := store.NewBankStatements(pool)
	var syncer httpapi.SberSyncer
	sberInfo := httpapi.SberInfo{Ctx: ctx, Days: cfg.Sber.SyncDays}
	if cfg.Sber.Enabled() {
		sc, certExpires, err := newSberSyncer(cfg.Sber, sberStore, bankStatements, log)
		if err != nil {
			return err
		}
		syncer = sc
		sberInfo.Configured, sberInfo.CertExpires, sberInfo.Interval = true, &certExpires, cfg.Sber.SyncInterval
		go sc.Run(ctx, cfg.Sber.SyncInterval, cfg.Sber.SyncDays)
	} else {
		log.Info("sber api is not configured (SBER_CLIENT_ID is empty)")
	}

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
			BankStatements:     bankStatements,
			Sber:               sberStore,
			SberSyncer:         syncer,
			SberInfo:           sberInfo,
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

// newSberSyncer собирает клиент Sber API (mTLS, токены в БД) и синхронизатор выписок.
func newSberSyncer(c config.Sber, sber *store.Sber, statements *store.BankStatements, log *slog.Logger) (*sbersync.Syncer, time.Time, error) {
	secret, err := os.ReadFile(c.ClientSecretFile)
	if err != nil {
		return nil, time.Time{}, err
	}
	httpClient, certExpires, err := sberapi.NewHTTPClient(sberapi.TLSFiles{P12: c.TLSP12, PasswordFile: c.TLSP12PassFile, CADir: c.CADir})
	if err != nil {
		return nil, time.Time{}, err
	}
	if left := time.Until(certExpires); left < 30*24*time.Hour {
		log.Warn("sber client certificate expires soon", "expires", certExpires.Format("2006-01-02"))
	}
	client := sberapi.New(sberapi.Config{BaseURL: c.BaseURL, ClientID: c.ClientID, ClientSecret: strings.TrimSpace(string(secret))}, httpClient, sber, log)
	return &sbersync.Syncer{Source: client, Statements: statements, Journal: sber, Tokens: sber, Log: log}, certExpires, nil
}
