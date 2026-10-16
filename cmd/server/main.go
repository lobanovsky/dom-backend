package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	_ "time/tzdata" // часовой пояс расписания не зависит от tzdata в образе

	"golang.org/x/crypto/bcrypt"

	"dom-backend/internal/auth"
	"dom-backend/internal/config"
	"dom-backend/internal/db"
	"dom-backend/internal/httpapi"
	"dom-backend/internal/logx"
	"dom-backend/internal/mailfetch"
	"dom-backend/internal/mailsync"
	"dom-backend/internal/registryimport"
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

	users := store.NewUsers(pool)
	if err := seedAdmin(ctx, users, cfg, log); err != nil {
		return err
	}
	a := auth.New(users, cfg.AdminSessionSecret, cfg.SessionTTL)

	premises := store.NewPremisesStore(pool)
	ownerships := store.NewOwnerships(pool)
	accounts := store.NewAccounts(pool)
	meterReadings := store.NewMeterReadings(pool)
	accountServices := store.NewAccountServices(pool)
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
		sberInfo.Configured, sberInfo.CertExpires, sberInfo.SyncAt, sberInfo.SyncTZ = true, &certExpires, cfg.Sber.SyncAt, cfg.Sber.SyncLocation.String()
		go sc.Run(ctx, cfg.Sber.SyncAt, cfg.Sber.SyncLocation, cfg.Sber.SyncDays)
	} else {
		log.Info("sber api is not configured (SBER_CLIENT_ID is empty)")
	}

	mailStore := store.NewMail(pool)
	if err := mailStore.AbortUnfinished(ctx); err != nil {
		return err
	}
	var mailSyncer httpapi.MailSyncer
	mailInfo := httpapi.MailInfo{Ctx: ctx}
	if cfg.Mail.Enabled() {
		ms, err := newMailSyncer(cfg.Mail, mailStore, store.NewPaymentRegistries(pool), log)
		if err != nil {
			return err
		}
		mailSyncer = ms
		mailInfo.Configured, mailInfo.User, mailInfo.Folder, mailInfo.SinceDays = true, cfg.Mail.User, cfg.Mail.Folder, cfg.Mail.SinceDays
		if cfg.Mail.SyncEvery > 0 {
			mailInfo.Every = cfg.Mail.SyncEvery.String()
		}
		go ms.Run(ctx, cfg.Mail.SyncEvery)
	} else {
		log.Info("mail import is not configured (MAIL_USER is empty)")
	}

	srv := &http.Server{
		Addr: cfg.ListenAddr,
		Handler: httpapi.NewRouter(httpapi.Deps{
			Auth:                 a,
			Users:                users,
			Organizations:        store.NewOrganizations(pool),
			Buildings:            store.NewBuildings(pool),
			Premises:             premises,
			Persons:              store.NewPersons(pool),
			LegalEntities:        store.NewLegalEntities(pool),
			Ownerships:           ownerships,
			Residencies:          store.NewResidencies(pool),
			Accounts:             accounts,
			Holders:              store.NewAccountHolders(pool),
			Importer:             store.NewImporter(pool),
			Meters:               store.NewMeters(pool),
			MeterReadings:        meterReadings,
			AccountServices:      accountServices,
			AccountServiceBulk:   accountServices,
			Services:             store.NewServices(pool),
			ServiceTariffs:       store.NewServiceTariffs(pool),
			MeterReadingImporter: meterReadings,
			Properties:           store.NewProperties(pool),
			PaymentRegistries:    store.NewPaymentRegistries(pool),
			PaymentExports:       store.NewPaymentExports(pool),
			Counterparties:       store.NewCounterparties(pool),
			BankStatements:       bankStatements,
			Sber:                 sberStore,
			SberSyncer:           syncer,
			SberInfo:             sberInfo,
			Mail:                 mailStore,
			MailSyncer:           mailSyncer,
			MailInfo:             mailInfo,
			PaymentRules:         paymentRules,
			RuleOrder:            paymentRules,
			Assignments:          store.NewAssignments(pool),
			BankAccounts:         store.NewBankAccounts(pool),
			PaymentCategories:    store.NewPaymentCategories(pool),
			IncomingPayments:     store.NewIncomingPayments(pool),
			OutgoingPayments:     store.NewOutgoingPayments(pool),
			PremisesOwnerships:   ownerships,
			PremisesAccounts:     accounts,
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

// newMailSyncer собирает забор реестров из почты: пароль читается из файла и больше нигде не хранится.
func newMailSyncer(c config.Mail, journal *store.Mail, registries *store.PaymentRegistries, log *slog.Logger) (*mailsync.Syncer, error) {
	pass, err := os.ReadFile(c.PasswordFile)
	if err != nil {
		return nil, err
	}
	return &mailsync.Syncer{
		Mailbox:   mailsync.IMAP{Config: mailfetch.Config{Addr: c.Addr, User: c.User, Password: strings.TrimSpace(string(pass)), Folder: c.Folder, From: c.From}},
		Importer:  registryimport.Importer{Store: registries},
		Journal:   journal,
		Folder:    c.Folder,
		SinceDays: c.SinceDays,
		Log:       log,
	}, nil
}

// seedAdmin создаёт первого администратора из ADMIN_USERNAME/ADMIN_PASSWORD_HASH, пока в БД нет ни одного пользователя.
// Позже эти переменные ни на что не влияют: пользователи и пароли ведутся в БД (страница «Пользователи»).
func seedAdmin(ctx context.Context, users *store.Users, cfg config.Config, log *slog.Logger) error {
	if cfg.AdminUsername != "" && cfg.AdminPasswordHash != "" {
		if _, err := bcrypt.Cost([]byte(cfg.AdminPasswordHash)); err != nil {
			return fmt.Errorf("ADMIN_PASSWORD_HASH is not a bcrypt hash: %w", err)
		}
		created, err := users.EnsureAdmin(ctx, cfg.AdminUsername, cfg.AdminPasswordHash)
		if err != nil {
			return err
		}
		if created {
			log.Info("first admin created from ADMIN_USERNAME", "username", cfg.AdminUsername)
		}
	}
	list, err := users.List(ctx)
	if err != nil {
		return err
	}
	if len(list) == 0 {
		return errors.New("no users: set ADMIN_USERNAME and ADMIN_PASSWORD_HASH to create the first admin")
	}
	return nil
}
