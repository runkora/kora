package inferencegw

import "strings"

// priceMatch records how a pricing row was found for a route.
type priceMatch int

const (
	// matchExact is the row for the exact inference profile the route calls.
	matchExact priceMatch = iota + 1
	// matchBase is the row of the model's own ID, or its global profile.
	matchBase
	// matchRegion is another region's row of the same model: an approximation.
	matchRegion
	// matchBare is the same model priced under another provider: an approximation.
	matchBare
)

func (m priceMatch) String() string {
	switch m {
	case matchExact:
		return "exact"
	case matchBase:
		return "base"
	case matchRegion:
		return "region"
	case matchBare:
		return "bare"
	}
	return "unknown"
}

// pricingIndex resolves a route's price. The admin catalog, the model list and
// billing all resolve through it, so they cannot disagree about which row a
// route is priced by. It is immutable once built.
type pricingIndex[T any] struct {
	exact  map[string]T // key: pricingExactKey
	base   map[string]T // key: pricingKey, rows of the model's own ID and its global profile
	region map[string]T // key: pricingKey, every row (last-resort fallback across regions)
	bare   map[string]T // key: pricingBareKey (cross-provider fallback)
	priced func(T) bool
}

// newPricingIndex indexes pricing rows. provOf and idOf extract the provider and
// model ID from a row so it works for any row type, and priced reports whether a
// row carries any price. Where several rows share a key the winner is chosen by
// rank, never by the order of rows, so the same table always resolves the same
// prices.
func newPricingIndex[T any](rows []T, provOf func(T) ProviderName, idOf func(T) string, priced func(T) bool) *pricingIndex[T] {
	plainestFirst := func(a, b T) bool {
		al, ap, ai := barePricingRank(provOf(a), idOf(a))
		bl, bp, bi := barePricingRank(provOf(b), idOf(b))
		return barePricingLess(al, ap, ai, bl, bp, bi)
	}
	// The model's own ID and its global profile are the preferred stand-ins for a
	// profile with no row of its own. Another region's row is a last resort and
	// stays out of the cross-provider fallback.
	var shared []T
	for _, r := range rows {
		if !hasRegionPrefix(provOf(r), idOf(r)) || isGlobalProfile(provOf(r), idOf(r)) {
			shared = append(shared, r)
		}
	}
	return &pricingIndex[T]{
		// A versioned row and its bare alias share an exact key; the alias is
		// the plainest ID and wins.
		exact: bestRowByKey(rows,
			func(r T) string { return pricingExactKey(provOf(r), idOf(r)) },
			plainestFirst),
		// The model's own ID first, then the global profile.
		base: bestRowByKey(shared,
			func(r T) string { return pricingKey(provOf(r), idOf(r)) },
			func(a, b T) bool {
				ga, gb := isGlobalProfile(provOf(a), idOf(a)), isGlobalProfile(provOf(b), idOf(b))
				if ga != gb {
					return gb
				}
				return plainestFirst(a, b)
			}),
		// With nothing else to go on, the plainest ID of the model's regional
		// rows, so a given table always borrows the same region.
		region: bestRowByKey(rows,
			func(r T) string { return pricingKey(provOf(r), idOf(r)) },
			plainestFirst),
		bare:   bareFallbackIndex(shared, provOf, idOf),
		priced: priced,
	}
}

// Resolve returns the row pricing provider+modelID and how it was found. The
// exact inference profile wins. A route with no row of its own is priced by the
// model's own ID, then its global profile, then another region's row of the same
// model, then the same model under another provider. The last two are
// deliberately approximate, like the cross-provider fallback before them, and
// are used only when nothing nearer exists.
//
// A row with no price at all (an override-only row whose override was cleared,
// or a row the sync no longer lists) yields to any priced row further down, so it
// cannot shadow a real price with zero. With no priced row at all it is still
// returned, as a model can be free.
func (x *pricingIndex[T]) Resolve(provider ProviderName, modelID string) (T, priceMatch, bool) {
	exact, haveExact := lookupWithInstructAlias(x.exact, provider, pricingExactKey(provider, modelID))
	if haveExact && x.priced(exact) {
		return exact, matchExact, true
	}
	baseKey := pricingKey(provider, modelID)
	base, haveBase := lookupWithInstructAlias(x.base, provider, baseKey)
	if haveBase && x.priced(base) {
		return base, matchBase, true
	}
	other, haveOther := lookupWithInstructAlias(x.region, provider, baseKey)
	if haveOther && x.priced(other) {
		return other, matchRegion, true
	}
	switch {
	case haveExact:
		return exact, matchExact, true
	case haveBase:
		return base, matchBase, true
	case haveOther:
		return other, matchRegion, true
	}
	r, ok := x.bare[pricingBareKey(provider, modelID)]
	return r, matchBare, ok
}

// lookupWithInstructAlias looks key up, then for a Bedrock ID ending in
// "-instruct" the key without it: AWS aliases such as
// "qwen.qwen3-coder-480b-a35b-instruct" are stored without the suffix.
func lookupWithInstructAlias[T any](m map[string]T, provider ProviderName, key string) (T, bool) {
	if r, ok := m[key]; ok {
		return r, true
	}
	if provider.IsBedrockFamily() && bedrockInstructSuffix.MatchString(key) {
		r, ok := m[bedrockInstructSuffix.ReplaceAllString(key, "")]
		return r, ok
	}
	var none T
	return none, false
}

// isGlobalProfile reports whether modelID names a Bedrock global inference
// profile.
func isGlobalProfile(provider ProviderName, modelID string) bool {
	return provider.IsBedrockFamily() && strings.HasPrefix(modelID, "global.")
}

// bestRowByKey keeps, for each key, the row that sorts first under less.
func bestRowByKey[T any](rows []T, keyOf func(T) string, less func(a, b T) bool) map[string]T {
	best := make(map[string]T, len(rows))
	for _, r := range rows {
		k := keyOf(r)
		if cur, ok := best[k]; !ok || less(r, cur) {
			best[k] = r
		}
	}
	return best
}

// hasRegionPrefix reports whether modelID names a regional or global inference
// profile rather than the model's own ID.
func hasRegionPrefix(provider ProviderName, modelID string) bool {
	return stripPricingRegion(provider, modelID) != modelID
}
