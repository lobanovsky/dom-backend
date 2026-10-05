package httpapi

import (
	"net/http"

	"dom-backend/internal/auth"
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

	Importer ImportStore

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
	personHandlers{d.Persons}.register(api)
	legalEntityHandlers{d.LegalEntities}.register(api)
	ownershipHandlers{d.Ownerships}.register(api)
	residencyHandlers{d.Residencies}.register(api)
	accountHandlers{d.Accounts}.register(api)
	accountHolderHandlers{d.Holders}.register(api)
	mux.Handle("/api/v1/", a.require(api))
	return mux
}
