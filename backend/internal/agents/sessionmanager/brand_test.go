package sessionmanager

import "github.com/clear-route/orbit/internal/platform/brand"

// testBrand is a product name no default could produce, so text that carries
// it proves the injection rather than the base config.
var testBrand = brand.MustNew("Acme")
