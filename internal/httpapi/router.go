package httpapi

import (
	"net/http"

	"dom-backend/internal/auth"
	"dom-backend/internal/model"
)

type Deps struct {
	Auth  *auth.Auth
	Users UserStore

	Organizations OrganizationStore
	Buildings     BuildingStore
	Premises      PremisesStore
	Persons       PersonStore
	LegalEntities LegalEntityStore
	Ownerships    OwnershipStore
	Residencies   ResidencyStore
	Accounts      AccountStore
	Holders       AccountHolderStore

	Importer             ImportStore
	PaymentRegistries    PaymentRegistryStore
	PaymentExports       PaymentExportStore
	AdjustmentRegistries AdjustmentRegistryStore
	Counterparties       CounterpartyStore
	BankStatements       BankStatementStore
	PaymentRules         crudStore[model.PaymentRule, model.PaymentRuleFilter]
	RuleOrder            RuleOrderStore
	Assignments          AssignmentStore
	BankAccounts         crudStore[model.BankAccount, model.BankAccountFilter]
	PaymentCategories    crudStore[model.PaymentCategory, model.PaymentCategoryFilter]
	PremisesKinds        crudStore[model.PremisesKind, model.PremisesKindFilter]
	IncomingPayments     crudStore[model.IncomingPayment, model.IncomingPaymentFilter]
	OutgoingPayments     crudStore[model.OutgoingPayment, model.OutgoingPaymentFilter]
	Meters               crudStore[model.Meter, model.MeterFilter]
	MeterReadings        crudStore[model.MeterReading, model.MeterReadingFilter]
	AccountServices      crudStore[model.AccountService, model.AccountServiceFilter]
	AccountServiceBulk   AccountServiceBulkStore
	Services             crudStore[model.Service, model.ServiceFilter]
	ServiceTariffs       crudStore[model.ServiceTariff, model.ServiceTariffFilter]
	ServiceNorms         crudStore[model.ServiceNorm, model.ServiceNormFilter]
	Tariffs              crudStore[model.Tariff, model.TariffFilter]
	TariffPrices         crudStore[model.TariffPrice, model.TariffPriceFilter]
	Calculator           CalculationStore
	MeterReadingImporter MeterReadingImportStore
	Properties           PropertiesStore
	Sber                 SberStore
	SberSyncer           SberSyncer
	SberInfo             SberInfo
	Mail                 MailStore
	MailSyncer           MailSyncer
	MailInfo             MailInfo

	PremisesOwnerships PremisesOwnershipsStore
	PremisesAccounts   PremisesAccountsStore
}

func NewRouter(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	a := &authHandlers{auth: d.Auth, users: d.Users}
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("POST /api/v1/auth/logout", a.logout)

	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/auth/me", a.me)
	api.HandleFunc("PUT /api/v1/auth/password", a.changePassword)
	userHandlers{d.Users, a}.register(api)
	organizationHandlers{d.Organizations}.register(api)
	buildingHandlers{d.Buildings}.register(api)
	premisesHandlers{d.Premises}.register(api)
	premisesNestedHandlers{d.PremisesOwnerships, d.PremisesAccounts}.register(api)
	importHandlers{d.Importer}.register(api)
	propertiesHandlers{d.Properties}.register(api)
	paymentRegistryHandlers{d.PaymentRegistries}.register(api)
	paymentExportHandlers{d.PaymentExports}.register(api)
	adjustmentRegistryHandlers{d.AdjustmentRegistries}.register(api)
	counterpartyHandlers{d.Counterparties}.register(api)
	bankStatementHandlers{d.BankStatements}.register(api)
	sberHandlers{d.Sber, d.SberSyncer, d.SberInfo}.register(api)
	mailHandlers{d.Mail, d.MailSyncer, d.MailInfo}.register(api)
	crudHandlers[model.PaymentRule, model.PaymentRuleFilter]{path: "/api/v1/payment-rules", store: d.PaymentRules, filter: paymentRuleFilter, prepare: func(r *model.PaymentRule) { r.SetDefaults() }}.register(api)
	assignmentHandlers{d.Assignments, d.RuleOrder}.register(api)
	crudHandlers[model.BankAccount, model.BankAccountFilter]{path: "/api/v1/bank-accounts", store: d.BankAccounts, filter: bankAccountFilter, prepare: func(a *model.BankAccount) { a.SetDefaults() }}.register(api)
	crudHandlers[model.PaymentCategory, model.PaymentCategoryFilter]{path: "/api/v1/payment-categories", store: d.PaymentCategories, filter: paymentCategoryFilter}.register(api)
	crudHandlers[model.PremisesKind, model.PremisesKindFilter]{path: "/api/v1/premises-kinds", store: d.PremisesKinds, filter: premisesKindFilter}.register(api)
	crudHandlers[model.Meter, model.MeterFilter]{path: "/api/v1/meters", store: d.Meters, filter: meterFilter, prepare: func(m *model.Meter) { m.SetDefaults() }}.register(api)
	meterImportHandlers{d.MeterReadingImporter}.register(api)
	accountServiceBulkHandlers{d.AccountServiceBulk}.register(api)
	crudHandlers[model.AccountService, model.AccountServiceFilter]{path: "/api/v1/account-services", store: d.AccountServices, filter: accountServiceFilter}.register(api)
	crudHandlers[model.Service, model.ServiceFilter]{path: "/api/v1/services", store: d.Services, filter: serviceFilter, prepare: func(s *model.Service) { s.SetDefaults() }}.register(api)
	crudHandlers[model.ServiceTariff, model.ServiceTariffFilter]{path: "/api/v1/service-tariffs", store: d.ServiceTariffs, filter: serviceTariffFilter, readOnly: true}.register(api)
	crudHandlers[model.Tariff, model.TariffFilter]{path: "/api/v1/tariffs", store: d.Tariffs, filter: tariffFilter}.register(api)
	crudHandlers[model.TariffPrice, model.TariffPriceFilter]{path: "/api/v1/tariff-prices", store: d.TariffPrices, filter: tariffPriceFilter}.register(api)
	calculationHandlers{d.Calculator}.register(api)
	crudHandlers[model.ServiceNorm, model.ServiceNormFilter]{path: "/api/v1/service-norms", store: d.ServiceNorms, filter: serviceNormFilter}.register(api)
	crudHandlers[model.MeterReading, model.MeterReadingFilter]{path: "/api/v1/meter-readings", store: d.MeterReadings, filter: meterReadingFilter, prepare: func(r *model.MeterReading) { r.SetDefaults() }}.register(api)
	paymentExcelHandlers{d.IncomingPayments, d.OutgoingPayments, d.BankAccounts, d.PaymentCategories}.register(api)
	crudHandlers[model.IncomingPayment, model.IncomingPaymentFilter]{path: "/api/v1/incoming-payments", store: d.IncomingPayments, filter: incomingPaymentFilter}.register(api)
	crudHandlers[model.OutgoingPayment, model.OutgoingPaymentFilter]{path: "/api/v1/outgoing-payments", store: d.OutgoingPayments, filter: outgoingPaymentFilter}.register(api)
	personHandlers{d.Persons}.register(api)
	legalEntityHandlers{d.LegalEntities}.register(api)
	ownershipHandlers{d.Ownerships}.register(api)
	residencyHandlers{d.Residencies}.register(api)
	accountHandlers{d.Accounts}.register(api)
	accountHolderHandlers{d.Holders}.register(api)
	mux.Handle("/api/v1/", a.require(api))
	return logRequests(mux)
}
