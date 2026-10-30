package skills

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/clear-route/orbit/internal/platform/logging"
)

// ORBIT_TEST_REDIS_URL enables tests against a real Redis with Search/BM25STD.
// Tests use unique scopes and delete only their own feature-prefixed records.
func redisSearchForTest(t testing.TB, ttl time.Duration) (*SearchIndex, string) {
	t.Helper()
	address := os.Getenv("ORBIT_TEST_REDIS_URL")
	if address == "" {
		t.Skip("set ORBIT_TEST_REDIS_URL to run Redis Search integration tests")
	}
	options, err := redis.ParseURL(address)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	t.Cleanup(func() { _ = client.Close() })
	index := &SearchIndex{redis: client, ttl: ttl}
	if err := index.EnsureIndex(t.Context()); err != nil {
		t.Fatal(err)
	}
	return index, "search-test-" + newSearchID()
}

func replaceSearchForTest(t testing.TB, index *SearchIndex, tenant, marketplace string, skills []Skill) {
	t.Helper()
	scope := searchScope(tenant, marketplace)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, pattern := range []string{
			searchDocPrefix + scope + ":*",
			searchReadyPrefix + scope,
			searchResultPrefix + scope + ":*",
			MarketplaceCacheKey(tenant, marketplace),
		} {
			var cursor uint64
			for {
				keys, next, err := index.redis.Scan(ctx, cursor, pattern, 100).Result()
				if err != nil {
					t.Error(err)
					break
				}
				if len(keys) > 0 {
					if err := index.redis.Del(ctx, keys...).Err(); err != nil {
						t.Error(err)
					}
				}
				cursor = next
				if cursor == 0 {
					break
				}
			}
		}
	})
	if err := index.redis.Set(t.Context(), MarketplaceCacheKey(tenant, marketplace), "[]", index.ttl).Err(); err != nil {
		t.Fatal(err)
	}
	if err := index.ReplaceMarketplace(t.Context(), tenant, marketplace, skills); err != nil {
		t.Fatal(err)
	}
}

func searchNames(t *testing.T, index *SearchIndex, tenant, marketplace, query string) []string {
	t.Helper()
	result, err := index.Search(t.Context(), tenant, marketplace, query, "")
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(result.Skills))
	for i, skill := range result.Skills {
		names[i] = skill.Name
	}
	return names
}

func TestRedisSearch_FieldMatchingAndBM25Weights(t *testing.T) {
	index, tenant := redisSearchForTest(t, time.Minute)
	skills := []Skill{
		{Name: "Orchestration", Description: "Catalogue", SkillSlug: "repo__a",
			File: SkillFile{Content: "---\ncustom: metadataonly\n---\nOverview"}},
		{Name: "Second", Description: "Orchestration", SkillSlug: "repo__b",
			File: SkillFile{Content: "Overview"}},
		{Name: "Third", Description: "Catalogue", SkillSlug: "repo__c",
			File: SkillFile{Content: "Orchestration"}},
	}
	replaceSearchForTest(t, index, tenant, "repo", skills)
	got := searchNames(t, index, tenant, "repo", "ORCHESTRATION")
	if !slices.Equal(got, []string{"Orchestration", "Second", "Third"}) {
		t.Fatalf("weighted matches = %v", got)
	}
	result, err := index.Search(t.Context(), tenant, "repo", "orchestration", "")
	if err != nil || len(result.Skills) != 3 || result.Skills[0].SkillName != "a" {
		t.Fatalf("search summary lost skillName: %+v, %v", result, err)
	}
	if got := searchNames(t, index, tenant, "repo", "metadataonly"); len(got) != 0 {
		t.Fatalf("frontmatter leaked into the indexed body: %v", got)
	}
	// Words can match across fields; every word is still required.
	skills[0].Description = "Bluequartz"
	replaceSearchForTest(t, index, tenant, "repo", skills)
	if got := searchNames(t, index, tenant, "repo", "orchestration bluequartz"); !slices.Equal(got, []string{"Orchestration"}) {
		t.Fatalf("cross-field query = %v", got)
	}
}

func TestRedisSearch_FullTextBeyond128KiB(t *testing.T) {
	index, tenant := redisSearchForTest(t, time.Minute)
	const formerLimit = 128 * 1024
	padding := strings.Repeat("проверка ", formerLimit/len("проверка ")+1)
	skill := Skill{
		Name:        "namebeacon " + padding + " nametailmarker",
		Description: "descriptionbeacon " + padding + " descriptiontailmarker",
		SkillSlug:   "repo__complete",
		File:        SkillFile{Content: "---\ncustom: metadatabeacon\n---\nbodybeacon " + padding + " bodytailmarker"},
	}
	replaceSearchForTest(t, index, tenant, "repo", []Skill{skill})
	for _, query := range []string{
		"namebeacon", "descriptionbeacon", "bodybeacon",
		"nametailmarker", "descriptiontailmarker", "bodytailmarker",
	} {
		result, err := index.Search(t.Context(), tenant, "repo", query, "")
		if err != nil || len(result.Skills) != 1 {
			t.Fatalf("full text lost %q: matches=%d, error=%v", query, len(result.Skills), err)
		}
		if result.Skills[0].Name != skill.Name || result.Skills[0].Description != skill.Description {
			t.Fatal("search summary shortened the name or description")
		}
	}
	if got := searchNames(t, index, tenant, "repo", "metadatabeacon"); len(got) != 0 {
		t.Fatal("frontmatter leaked into the indexed body")
	}
}

func TestRedisSearch_ContinuationReadsOnlyRequestedBatch(t *testing.T) {
	index, tenant := redisSearchForTest(t, time.Minute)
	skills := make([]Skill, 450)
	for i := range skills {
		skills[i] = Skill{Name: "Needle", SkillSlug: fmt.Sprintf("repo__%03d", i)}
	}
	replaceSearchForTest(t, index, tenant, "repo", skills)
	first, err := index.Search(t.Context(), tenant, "repo", "needle", "")
	if err != nil || first.NextCursor == "" {
		t.Fatalf("first batch: %+v, %v", first, err)
	}
	cursor, err := decodeSearchCursor(first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	key := searchResultKey(searchScope(tenant, "repo"), searchDigest("needle"), cursor.Snapshot)
	// An unreadable later batch must not be decoded while reading this one.
	if err := index.redis.HSet(t.Context(), key, "400", "invalid later batch").Err(); err != nil {
		t.Fatal(err)
	}
	second, err := index.Search(t.Context(), tenant, "repo", "needle", first.NextCursor)
	if err != nil || len(second.Skills) != serverCap || second.Skills[0].SkillSlug != "repo__200" {
		t.Fatalf("continuation read beyond its batch: %+v, %v", second, err)
	}
	ttl, err := index.redis.PTTL(t.Context(), key).Result()
	if err != nil || ttl <= 0 || ttl > time.Minute {
		t.Fatalf("snapshot expiry exceeded source lifetime: %v, %v", ttl, err)
	}
	generation, err := index.generation(t.Context(), searchScope(tenant, "repo"))
	if err != nil {
		t.Fatal(err)
	}
	expiresAt, err := index.redis.PExpireTime(t.Context(), key).Result()
	if err != nil || expiresAt.Milliseconds() != generation.ExpiresAt {
		t.Fatalf("snapshot lost the generation deadline: %v, %v", expiresAt, err)
	}
	_, err = index.Search(t.Context(), tenant, "repo", "needle", encodeSearchCursor(cursor.Snapshot, 600))
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("missing continuation batch accepted: %v", err)
	}
}

func TestRedisSearch_ClosestMatches(t *testing.T) {
	index, tenant := redisSearchForTest(t, time.Minute)
	replaceSearchForTest(t, index, tenant, "repo", []Skill{
		{Name: "Quartzprobe", Description: "Catalogue", SkillSlug: "repo__title", File: SkillFile{Content: "Overview"}},
		{Name: "Second", Description: "Quartzprobe", SkillSlug: "repo__description", File: SkillFile{Content: "Overview"}},
		{Name: "Third", Description: "Catalogue", SkillSlug: "repo__body", File: SkillFile{Content: "Quartzprobe"}},
	})
	for _, query := range []string{"quartzprobe", "quartz", "quartzprbe", "quortzprobe"} {
		t.Run(query, func(t *testing.T) {
			got := searchNames(t, index, tenant, "repo", query)
			if !slices.Equal(got, []string{"Quartzprobe", "Second", "Third"}) {
				t.Fatalf("closest matches for %q = %v", query, got)
			}
		})
	}
	for _, query := range []string{"zzznomatch", "quartz missingword", "quartzprbe missingword"} {
		if got := searchNames(t, index, tenant, "repo", query); len(got) != 0 {
			t.Fatalf("unrelated words were ignored for %q: %v", query, got)
		}
	}
}

func TestRedisSearch_ClosestMatchesPreserveNormalResultsAndScope(t *testing.T) {
	index, tenant := redisSearchForTest(t, time.Minute)
	replaceSearchForTest(t, index, tenant, "repo", []Skill{
		{Name: "Exact body", SkillSlug: "repo__body", File: SkillFile{Content: "Quartzprobe"}},
		{Name: "Quartzprobetools", SkillSlug: "repo__prefix"},
		{Name: "Quortzprobe", SkillSlug: "repo__typo"},
	})
	if got := searchNames(t, index, tenant, "repo", "quartzprobe"); !slices.Equal(got, []string{"Exact body"}) {
		t.Fatalf("approximate titles displaced a normal body match: %v", got)
	}
	for _, scope := range []struct{ tenant, marketplace string }{
		{tenant + "-other", "repo"}, {tenant, "other-repo"},
	} {
		replaceSearchForTest(t, index, scope.tenant, scope.marketplace, []Skill{{Name: "Isolatedneedle", SkillSlug: "skill"}})
	}
	for _, query := range []string{"isolated", "isolatednedle"} {
		if got := searchNames(t, index, tenant, "repo", query); len(got) != 0 {
			t.Fatalf("closest match escaped its scope for %q: %v", query, got)
		}
	}
}

func TestRedisSearch_ClosestMatchesContinueAcrossBatches(t *testing.T) {
	index, tenant := redisSearchForTest(t, time.Minute)
	skills := make([]Skill, 250)
	for i := range skills {
		skills[i] = Skill{Name: "Quartzprobe", SkillSlug: fmt.Sprintf("repo__%03d", i)}
	}
	replaceSearchForTest(t, index, tenant, "repo", skills)
	for _, query := range []string{"quartz", "quartzprbe"} {
		first, err := index.Search(t.Context(), tenant, "repo", query, "")
		if err != nil || len(first.Skills) != serverCap || first.NextCursor == "" {
			t.Fatalf("first approximate batch for %q: %+v, %v", query, first, err)
		}
		second, err := index.Search(t.Context(), tenant, "repo", query, first.NextCursor)
		if err != nil || len(second.Skills) != 50 || second.NextCursor != "" {
			t.Fatalf("last approximate batch for %q: %+v, %v", query, second, err)
		}
		all := append(first.Skills, second.Skills...)
		for i, skill := range all {
			if skill.SkillSlug != skills[i].SkillSlug || skill.SkillName == "" {
				t.Fatalf("missing or reordered approximate result at %d: %+v", i, skill)
			}
		}
	}
}

func TestRedisSearch_ScopeAndLiteralQueries(t *testing.T) {
	index, tenant := redisSearchForTest(t, time.Minute)
	for _, scope := range []struct{ tenant, marketplace, name string }{
		{tenant, "repo", "Allowed"},
		{tenant + "-other", "repo", "Other tenant"},
		{tenant, "other-repo", "Other marketplace"},
	} {
		replaceSearchForTest(t, index, scope.tenant, scope.marketplace, []Skill{
			{Name: scope.name, SkillSlug: "skill", File: SkillFile{Content: "Needle"}},
		})
	}
	for _, query := range []string{"needle", "needle | *", "needle*"} {
		if got := searchNames(t, index, tenant, "repo", query); !slices.Equal(got, []string{"Allowed"}) {
			t.Fatalf("query %q escaped its scope: %v", query, got)
		}
	}
	for _, query := range []string{"***", "@scope:{other}", "needle nonexistent", "the"} {
		if got := searchNames(t, index, tenant, "repo", query); len(got) != 0 {
			t.Fatalf("literal/no-match query %q = %v", query, got)
		}
	}
}

func TestRedisSearch_ReplacementAndEmptyReadiness(t *testing.T) {
	index, tenant := redisSearchForTest(t, time.Minute)
	_, err := index.Search(t.Context(), tenant, "repo", "needle", "")
	if !errors.Is(err, ErrSearchIndexMissing) {
		t.Fatalf("cold search = %v", err)
	}
	replaceSearchForTest(t, index, tenant, "repo", []Skill{{Name: "Needle", SkillSlug: "old"}})
	if got := searchNames(t, index, tenant, "repo", "needle"); len(got) != 1 {
		t.Fatal(got)
	}
	replaceSearchForTest(t, index, tenant, "repo", []Skill{})
	if got := searchNames(t, index, tenant, "repo", "needle"); len(got) != 0 {
		t.Fatalf("removed skill still searchable: %v", got)
	}
	generation, err := index.generation(t.Context(), searchScope(tenant, "repo"))
	if err != nil || generation.Count != 0 {
		t.Fatalf("empty generation was not marked ready: %+v, %v", generation, err)
	}
}

func TestRedisSearch_SnapshotPaginationAndCursorIsolation(t *testing.T) {
	index, tenant := redisSearchForTest(t, time.Minute)
	skills := make([]Skill, 450)
	for i := range skills {
		skills[i] = Skill{Name: "Needle", Description: "Catalogue",
			SkillSlug: fmt.Sprintf("repo__%03d", i), File: SkillFile{Content: "Overview"}}
	}
	replaceSearchForTest(t, index, tenant, "repo", skills)
	first, err := index.Search(t.Context(), tenant, "repo", "needle", "")
	if err != nil || len(first.Skills) != serverCap || first.NextCursor == "" {
		t.Fatalf("first batch: %d, %v", len(first.Skills), err)
	}
	for _, call := range []searchCall{
		{tenant + "-other", "repo", "needle", first.NextCursor},
		{tenant, "other", "needle", first.NextCursor},
		{tenant, "repo", "different", first.NextCursor},
	} {
		_, err := index.Search(t.Context(), call.tenant, call.marketplace, call.query, call.cursor)
		if !errors.Is(err, ErrInvalidCursor) {
			t.Fatalf("cursor accepted for a different request: %+v, %v", call, err)
		}
	}
	// A refresh changes new searches while in-progress batches stay fixed.
	replaceSearchForTest(t, index, tenant, "repo", []Skill{{Name: "Needle new", SkillSlug: "new"}})
	second, err := index.Search(t.Context(), tenant, "repo", "NEEDLE", first.NextCursor)
	if err != nil || len(second.Skills) != serverCap {
		t.Fatalf("second batch: %d, %v", len(second.Skills), err)
	}
	third, err := index.Search(t.Context(), tenant, "repo", "needle", second.NextCursor)
	if err != nil || len(third.Skills) != 50 || third.NextCursor != "" {
		t.Fatalf("last batch: %d, %v", len(third.Skills), err)
	}
	all := append(append(first.Skills, second.Skills...), third.Skills...)
	for i, skill := range all {
		if skill.SkillSlug != skills[i].SkillSlug {
			t.Fatalf("duplicate, missing, or reordered match at %d: %s", i, skill.SkillSlug)
		}
	}
	if got := searchNames(t, index, tenant, "repo", "needle"); !slices.Equal(got, []string{"Needle new"}) {
		t.Fatalf("new search did not see refresh: %v", got)
	}
	cursor, err := decodeSearchCursor(first.NextCursor)
	if err != nil {
		t.Fatal(err)
	}
	key := searchResultKey(searchScope(tenant, "repo"), searchDigest("needle"), cursor.Snapshot)
	if err := index.redis.Del(t.Context(), key).Err(); err != nil {
		t.Fatal(err)
	}
	_, err = index.Search(t.Context(), tenant, "repo", "needle", first.NextCursor)
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expired cursor = %v", err)
	}
}

func TestRedisSearch_DocumentsAndReadinessExpire(t *testing.T) {
	index, tenant := redisSearchForTest(t, 150*time.Millisecond)
	replaceSearchForTest(t, index, tenant, "repo", []Skill{{Name: "Needle", SkillSlug: "one"}})
	scope := searchScope(tenant, "repo")
	keys, err := index.redis.Keys(t.Context(), searchDocPrefix+scope+":*").Result()
	if err != nil || len(keys) != 1 {
		t.Fatalf("document keys: %v, %v", keys, err)
	}
	for _, key := range append(keys, searchReadyPrefix+scope) {
		ttl, err := index.redis.PTTL(t.Context(), key).Result()
		if err != nil || ttl <= 0 || ttl > 150*time.Millisecond {
			t.Fatalf("key has wrong lifetime: %v, %v", ttl, err)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		_, err := index.Search(t.Context(), tenant, "repo", "needle", "")
		if errors.Is(err, ErrSearchIndexMissing) {
			break
		}
		if err != nil || time.Now().After(deadline) {
			t.Fatalf("generation did not expire: %v", err)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestRedisSearch_BackfillRetainsSourceExpiry(t *testing.T) {
	index, tenant := redisSearchForTest(t, time.Minute)
	replaceSearchForTest(t, index, tenant, "repo", []Skill{{Name: "Needle", SkillSlug: "one"}})
	// The source cache is near expiry even though the configured TTL is longer.
	key := MarketplaceCacheKey(tenant, "repo")
	if err := index.redis.PExpire(t.Context(), key, time.Second).Err(); err != nil {
		t.Fatal(err)
	}
	if err := index.ReplaceMarketplace(t.Context(), tenant, "repo", []Skill{{Name: "Needle", SkillSlug: "one"}}); err != nil {
		t.Fatal(err)
	}
	generation, err := index.generation(t.Context(), searchScope(tenant, "repo"))
	if err != nil || generation.lifetime() > time.Second {
		t.Fatalf("backfill extended source freshness: %+v, %v", generation, err)
	}
}

func TestRedisSearch_NonExpiringCacheHasBoundedProjection(t *testing.T) {
	index, tenant := redisSearchForTest(t, 0)
	replaceSearchForTest(t, index, tenant, "repo", []Skill{{Name: "Needle", SkillSlug: "repo__one"}})
	scope := searchScope(tenant, "repo")
	keys, err := index.redis.Keys(t.Context(), searchDocPrefix+scope+":*").Result()
	if err != nil || len(keys) != 1 {
		t.Fatalf("document keys: %v, %v", keys, err)
	}
	for _, key := range append(keys, searchReadyPrefix+scope) {
		ttl, err := index.redis.PTTL(t.Context(), key).Result()
		if err != nil || ttl <= 0 || ttl > nonExpiringCacheSearchTTL {
			t.Fatalf("unbounded search projection: %v, %v", ttl, err)
		}
	}
	if got := searchNames(t, index, tenant, "repo", "needle"); !slices.Equal(got, []string{"Needle"}) {
		t.Fatalf("search over non-expiring cache: %v", got)
	}
}

func TestRedisSearch_HTTPBodySearchAndValidation(t *testing.T) {
	index, tenant := redisSearchForTest(t, time.Minute)
	source := handlerSource
	skill := Skill{Name: "Authentication Review", Description: "Inspect credentials",
		SkillSlug: source.MarketplaceSlug + "__authentication",
		File:      SkillFile{Content: "---\nname: Authentication Review\n---\nCheck bluequartz tokens."}}
	replaceSearchForTest(t, index, tenant, source.MarketplaceSlug, []Skill{skill})
	registry := &handlerRegistry{sources: map[string][]MarketplaceSource{tenant: {source}}}
	cache := &handlerCache{}
	crawler := &handlerCrawler{}
	logger := logging.NewDevelopmentLogger()
	service := NewService(registry, cache, crawler, index, logger)
	fixture := &handlerFixture{
		Controller: &Controller{service: service, registry: registry, logger: logger},
		registry:   registry, cache: cache, crawler: crawler,
	}
	path := "/marketplaces/" + source.MarketplaceSlug + "?search="
	response := handlerRequest(fixture, path+"bluequartz", tenant)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Authentication Review") ||
		strings.Contains(response.Body.String(), "Check bluequartz") {
		t.Fatalf("body search did not return only summaries: %d %s", response.Code, response.Body.String())
	}
	response = handlerRequest(fixture, path+strings.Repeat("x", maxSearchRunes+1), tenant)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("oversized query: %d %s", response.Code, response.Body.String())
	}
}
