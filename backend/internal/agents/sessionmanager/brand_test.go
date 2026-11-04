package sessionmanager

import "github.com/runkora/kora/backend/internal/platform/brand"

// testBrand is a name no default produces, so text carrying it proves injection.
var testBrand = brand.MustNew("Acme")
