package httpapi

import (
	"net/http"

	"dom-backend/internal/auth"
	"dom-backend/internal/model"
)

type Deps struct {
	Auth *auth.Auth

	Organizations OrganizationStore
	Buildings     BuildingStore
	Premises      PremisesStore
	Persons       PersonStore
	LegalEntities LegalEntityStore
	Ownerships    OwnershipStore
	Residencies   ResidencyStore
	Accounts      AccountStore
	Holders       AccountHolderStore

	Importer          ImportStore
	PaymentRegistries PaymentRegistryStore
	BankStatements    BankStatementStore
	PaymentRules      crudStore[model.PaymentRule, model.PaymentRuleFilter]
	RuleOrder         RuleOrderStore
	Assignments       AssignmentStore
	BankAccounts      crudStore[model.BankAccount, model.BankAccountFilter]
	PaymentCategories crudStore[model.PaymentCategory, model.PaymentCategoryFilter]
	IncomingPayments  crudStore[model.IncomingPayment, model.IncomingPaymentFilter]
	OutgoingPayments  crudStore[model.OutgoingPayment, model.OutgoingPaymentFilter]
	Properties        PropertiesStore

	PremisesOwnerships PremisesOwnershipsStore
	PremisesAccounts   PremisesAccountsStore
}

func NewRouter(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	a := &authHandlers{auth: d.Auth}
	mux.HandleFunc("POST /api/v1/auth/login", a.login)
	mux.HandleFunc("POST /api/v1/auth/logout", a.logout)

	api := http.NewServeMux()
	api.HandleFunc("GET /api/v1/auth/me", a.me)
	organizationHandlers{d.Organizations}.register(api)
	buildingHandlers{d.Buildings}.register(api)
	premisesHandlers{d.Premises}.register(api)
	premisesNestedHandlers{d.PremisesOwnerships, d.PremisesAccounts}.register(api)
	importHandlers{d.Importer}.register(api)
	propertiesHandlers{d.Properties}.register(api)
	paymentRegistryHandlers{d.PaymentRegistries}.register(api)
	bankStatementHandlers{d.BankStatements}.register(api)
	crudHandlers[model.PaymentRule, model.PaymentRuleFilter]{path: "/api/v1/payment-rules", store: d.PaymentRules, filter: paymentRuleFilter, prepare: func(r *model.PaymentRule) { r.SetDefaults() }}.register(api)
	assignmentHandlers{d.Assignments, d.RuleOrder}.register(api)
	crudHandlers[model.BankAccount, model.BankAccountFilter]{path: "/api/v1/bank-accounts", store: d.BankAccounts, filter: bankAccountFilter}.register(api)
	crudHandlers[model.PaymentCategory, model.PaymentCategoryFilter]{path: "/api/v1/payment-categories", store: d.PaymentCategories, filter: paymentCategoryFilter}.register(api)
	crudHandlers[model.IncomingPayment, model.IncomingPaymentFilter]{path: "/api/v1/incoming-payments", store: d.IncomingPayments, filter: incomingPaymentFilter}.register(api)
	crudHandlers[model.OutgoingPayment, model.OutgoingPaymentFilter]{path: "/api/v1/outgoing-payments", store: d.OutgoingPayments, filter: outgoingPaymentFilter}.register(api)
	personHandlers{d.Persons}.register(api)
	legalEntityHandlers{d.LegalEntities}.register(api)
	ownershipHandlers{d.Ownerships}.register(api)
	residencyHandlers{d.Residencies}.register(api)
	accountHandlers{d.Accounts}.register(api)
	accountHolderHandlers{d.Holders}.register(api)
	mux.Handle("/api/v1/", a.require(api))
	return mux
}
