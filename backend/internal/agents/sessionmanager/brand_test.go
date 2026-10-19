package sessionmanager

import "github.com/clear-route/orbit/internal/platform/brand"

// testBrand is a name no default produces, so text that carries it proves the
// brand was injected.
var testBrand = brand.MustNew("Acme")
