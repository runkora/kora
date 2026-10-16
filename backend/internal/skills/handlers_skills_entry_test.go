package skills

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/clear-route/orbit/internal/platform/api"
	ghclient "github.com/clear-route/orbit/internal/platform/clients/github"
	"github.com/clear-route/orbit/internal/platform/logging"
	"github.com/clear-route/orbit/internal/platform/policy"
	"github.com/clear-route/orbit/internal/platform/token"
)

var handlerSource = MarketplaceSource{
	Name:            "Clear Route Skills",
	Org:             "internal-org",
	RepoName:        "internal-repo",
	MarketplaceSlug: "internal-org__internal-repo",
	Link:            "https://github.com/internal-org/internal-repo",
}

// --- fakes ---

type handlerRegistry struct {
	sources map[string][]MarketplaceSource
	calls   []string
}

func (f *handlerRegistry) ListSources(tenantID string) []MarketplaceSource {
	f.calls = append(f.calls, tenantID)
	return f.sources[tenantID]
}

type handlerCache struct {
	marketplace map[string][]SkillEntry // key = marketplaceSlug
	skills      map[string]Skill        // key = skillSlug
	getCalls    []string
	setCalls    []string
	getError    error
	setErr      error
}

func (f *handlerCache) GetMarketplace(_ context.Context, tenantID, slug string) ([]SkillEntry, bool, error) {
	f.getCalls = append(f.getCalls, MarketplaceCacheKey(tenantID, slug))
	if f.getError != nil {
		return nil, false, f.getError
	}
	entries, hit := f.marketplace[slug]
	return entries, hit, nil
}

func (f *handlerCache) SetMarketplace(_ context.Context, tenantID, slug string, entries []SkillEntry) error {
	f.setCalls = append(f.setCalls, MarketplaceCacheKey(tenantID, slug))
	if f.setErr != nil {
		return f.setErr
	}
	if f.marketplace == nil {
		f.marketplace = make(map[string][]SkillEntry)
	}
	f.marketplace[slug] = entries
	return nil
}

func (f *handlerCache) GetSkill(_ context.Context, slug string) (Skill, bool, error) {
	f.getCalls = append(f.getCalls, SkillCacheKey(slug))
	if f.getError != nil {
		return Skill{}, false, f.getError
	}
	skill, hit := f.skills[slug]
	return skill, hit, nil
}

func (f *handlerCache) SetSkill(_ context.Context, slug string, skill Skill) error {
	f.setCalls = append(f.setCalls, SkillCacheKey(slug))
	if f.setErr != nil {
		return f.setErr
	}
	if f.skills == nil {
		f.skills = make(map[string]Skill)
	}
	f.skills[slug] = skill
	return nil
}

type handlerCrawler struct {
	skills    []Skill
	skillsErr error
	fetchCalls int
}

func (f *handlerCrawler) FetchTree(_ context.Context, _ MarketplaceSource) (map[string]skillFilePath, error) {
	return map[string]skillFilePath{}, nil
}

func (f *handlerCrawler) FetchSkills(_ context.Context, _ MarketplaceSource, _ map[string]skillFilePath) ([]Skill, error) {
	f.fetchCalls++
	return f.skills, f.skillsErr
}

// handlerFixture bundles a Controller with the fakes for inspection.
type handlerFixture struct {
	*Controller
	registry *handlerRegistry
	cache    *handlerCache
	crawler  *handlerCrawler
}

func handlerSkills() []Skill {
	slug := handlerSource.MarketplaceSlug
	return []Skill{
		{Name: "React Expert", Description: "React development", SkillSlug: slug + "__react-expert", File: SkillFile{Path: "skills/react-expert/SKILL.md", Name: "SKILL.md", Content: "# React Expert"}},
		{Name: "React Testing", Description: "React tests", SkillSlug: slug + "__react-testing", File: SkillFile{Path: "skills/react-testing/SKILL.md", Name: "SKILL.md", Content: "# React Testing"}},
		{Name: "Go Expert", Description: "Go development", SkillSlug: slug + "__go-expert", File: SkillFile{Path: "skills/go-expert/SKILL.md", Name: "SKILL.md", Content: "# Go Expert"}},
	}
}

func handlerSkillEntries() []SkillEntry {
	slug := handlerSource.MarketplaceSlug
	return []SkillEntry{
		{Name: "React Expert", Description: "React development", SkillSlug: slug + "__react-expert", Path: "skills/react-expert/SKILL.md"},
		{Name: "React Testing", Description: "React tests", SkillSlug: slug + "__react-testing", Path: "skills/react-testing/SKILL.md"},
		{Name: "Go Expert", Description: "Go development", SkillSlug: slug + "__go-expert", Path: "skills/go-expert/SKILL.md"},
	}
}

func newHandlerController(cached bool) *handlerFixture {
	reg := &handlerRegistry{sources: map[string][]MarketplaceSource{"tenant-a": {handlerSource}}}
	cache := &handlerCache{}
	if cached {
		cache.marketplace = map[string][]SkillEntry{
			handlerSource.MarketplaceSlug: handlerSkillEntries(),
		}
	}
	crawler := &handlerCrawler{skills: handlerSkills()}
	logger := logging.NewDevelopmentLogger()
	svc := NewService(reg, cache, crawler, logger)
	c := &Controller{service: svc, registry: reg, logger: logger}
	return &handlerFixture{Controller: c, registry: reg, cache: cache, crawler: crawler}
}

func handlerRequest(fix *handlerFixture, target, tenantID string) *httptest.ResponseRecorder {
	ctx := token.WithClaims(context.Background(), &token.PlatformClaims{
		Orbit: token.OrbitClaims{TenantID: tenantID, PrincipalType: token.PrincipalTypeHuman},
	})
	mux := api.NewMux(ctx, fix.logger)
	fix.Boot(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil))
	return recorder
}

func TestControllerMarketplaces(t *testing.T) {
	for _, tenantID := range []string{"tenant-a", "tenant-b"} {
		t.Run(tenantID, func(t *testing.T) {
			fix := newHandlerController(true)
			rr := handlerRequest(fix, "/marketplaces", tenantID)
			if rr.Code != http.StatusOK {
				t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
			}
			var body struct {
				Marketplaces []map[string]any `json:"marketplaces"`
			}
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tenantID == "tenant-a" {
				if len(body.Marketplaces) != 1 {
					t.Fatalf("want 1 marketplace, got %v", body.Marketplaces)
				}
				if body.Marketplaces[0]["name"] != "Clear Route Skills" {
					t.Errorf("name = %v", body.Marketplaces[0]["name"])
				}
				if body.Marketplaces[0]["marketplaceSlug"] != "internal-org__internal-repo" {
					t.Errorf("marketplaceSlug = %v", body.Marketplaces[0]["marketplaceSlug"])
				}
				if body.Marketplaces[0]["link"] != "https://github.com/internal-org/internal-repo" {
					t.Errorf("link = %v", body.Marketplaces[0]["link"])
				}
			} else {
				if len(body.Marketplaces) != 0 {
					t.Fatalf("unknown tenant must get [], got %v", body.Marketplaces)
				}
			}
			if len(fix.cache.getCalls) != 0 || fix.crawler.fetchCalls != 0 {
				t.Fatal("marketplace listing must not call Redis or GitHub")
			}
		})
	}
}

func TestControllerListSkills(t *testing.T) {
	fix := newHandlerController(true)
	slug := handlerSource.MarketplaceSlug
	rr := handlerRequest(fix, "/marketplaces/"+slug+"/skills?search=REACT", "tenant-a")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Skills []map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Skills) != 2 {
		t.Fatalf("want 2 react skills, got %d: %s", len(body.Skills), rr.Body.String())
	}
	// Each skill must include skillSlug.
	for _, s := range body.Skills {
		if _, ok := s["skillSlug"]; !ok {
			t.Errorf("skillSlug missing from list response: %v", s)
		}
	}
	if fix.crawler.fetchCalls != 0 {
		t.Fatal("cache hit caused a crawl")
	}
}

func TestControllerServerCap(t *testing.T) {
	fix := newHandlerController(false)
	items := make([]Skill, serverCap+10)
	for i := range items {
		items[i] = Skill{Name: fmt.Sprintf("skill-%03d", i), SkillSlug: handlerSource.MarketplaceSlug + fmt.Sprintf("__skill-%03d", i)}
	}
	fix.crawler.skills = items

	slug := handlerSource.MarketplaceSlug
	rr := handlerRequest(fix, "/marketplaces/"+slug+"/skills", "tenant-a")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Skills     []map[string]any `json:"skills"`
		NextCursor string           `json:"nextCursor"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Skills) != serverCap {
		t.Fatalf("got %d skills, want serverCap %d", len(body.Skills), serverCap)
	}
	if body.NextCursor == "" {
		t.Fatal("nextCursor missing when more than serverCap skills exist")
	}
}

func TestControllerPagination(t *testing.T) {
	fix := newHandlerController(false)
	items := make([]Skill, serverCap+10)
	for i := range items {
		items[i] = Skill{
			Name:      fmt.Sprintf("skill-%03d", i),
			SkillSlug: handlerSource.MarketplaceSlug + fmt.Sprintf("__skill-%03d", i),
		}
	}
	fix.crawler.skills = items
	base := "/marketplaces/" + handlerSource.MarketplaceSlug + "/skills"

	type page struct {
		Skills     []map[string]any `json:"skills"`
		NextCursor string           `json:"nextCursor"`
	}
	get := func(target string) page {
		rr := handlerRequest(fix, target, "tenant-a")
		if rr.Code != http.StatusOK {
			t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
		}
		var p page
		if err := json.Unmarshal(rr.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}

	first := get(base)
	if len(first.Skills) != serverCap || first.NextCursor == "" {
		t.Fatalf("first page: %d skills, cursor %q", len(first.Skills), first.NextCursor)
	}

	// Cursor is base64url, so it is safe in a query string unescaped.
	second := get(base + "?cursor=" + first.NextCursor)
	if len(second.Skills) != 10 || second.NextCursor != "" {
		t.Fatalf("second page: %d skills, cursor %q", len(second.Skills), second.NextCursor)
	}
	if fix.crawler.fetchCalls != 1 {
		t.Fatalf("crawls %d, want 1 (second page must hit cache)", fix.crawler.fetchCalls)
	}
}

func TestControllerInvalidCursor(t *testing.T) {
	fix := newHandlerController(true)
	target := "/marketplaces/" + handlerSource.MarketplaceSlug + "/skills?cursor=!!bad!!"
	rr := handlerRequest(fix, target, "tenant-a")
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400: %s", rr.Code, rr.Body.String())
	}
	if len(fix.cache.getCalls) != 0 {
		t.Fatal("invalid cursor reached the cache")
	}
}

func TestControllerGetSkill(t *testing.T) {
	fix := newHandlerController(true)
	slug := handlerSource.MarketplaceSlug
	skillSlug := slug + "__react-expert"
	rr := handlerRequest(fix, "/marketplaces/"+slug+"/skills/"+skillSlug, "tenant-a")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	// Detail response: name, description, skillSlug, skill (file), documents.
	for _, field := range []string{"name", "description", "skillSlug", "skill", "documents"} {
		if _, ok := body[field]; !ok {
			t.Errorf("field %q missing from detail response: %s", field, rr.Body.String())
		}
	}
}

func TestControllerNotFound(t *testing.T) {
	slug := handlerSource.MarketplaceSlug
	for _, tc := range []struct {
		name     string
		target   string
		tenantID string
		code     int
	}{
		{"unknown marketplace", "/marketplaces/unknown__slug/skills", "tenant-a", http.StatusNotFound},
		{"wrong tenant", "/marketplaces/" + slug + "/skills", "tenant-b", http.StatusNotFound},
		{"unknown skill", "/marketplaces/" + slug + "/skills/" + slug + "__missing", "tenant-a", http.StatusNotFound},
		{"wrong prefix skill", "/marketplaces/" + slug + "/skills/other__repo__skill", "tenant-a", http.StatusNotFound},
		{"slash in skill name", "/marketplaces/" + slug + "/skills/bad%2Fname", "tenant-a", http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fix := newHandlerController(true)
			rr := handlerRequest(fix, tc.target, tc.tenantID)
			if rr.Code != tc.code {
				t.Fatalf("status %d, want %d: %s", rr.Code, tc.code, rr.Body.String())
			}
			if fix.crawler.fetchCalls != 0 {
				t.Fatal("not-found request caused a crawl")
			}
		})
	}
}

func TestControllerReadFailures(t *testing.T) {
	slug := handlerSource.MarketplaceSlug
	for _, target := range []string{"/skills", "/skills/" + slug + "__react-expert"} {
		for _, tc := range []struct {
			name                   string
			getErr, crawlErr, setErr error
			code, crawls, writes   int
		}{
			{"Redis read", fmt.Errorf("private: %w", ErrRedisUnavailable), nil, nil, http.StatusServiceUnavailable, 0, 0},
			{"GitHub outage", nil, fmt.Errorf("private: %w", ghclient.ErrUnavailable), nil, http.StatusServiceUnavailable, 1, 0},
			{"Redis write", nil, nil, ErrRedisUnavailable, http.StatusServiceUnavailable, 1, 1},
			{"corrupt cache", errors.New("private JSON"), nil, nil, http.StatusInternalServerError, 0, 0},
			{"unexpected crawl error", nil, errors.New("private config"), nil, http.StatusInternalServerError, 1, 0},
			{"crawl timeout", nil, context.DeadlineExceeded, nil, http.StatusServiceUnavailable, 1, 0},
		} {
			t.Run(target+"/"+tc.name, func(t *testing.T) {
				fix := newHandlerController(false)
				fix.cache.getError = tc.getErr
				fix.cache.setErr = tc.setErr
				fix.crawler.skillsErr = tc.crawlErr
				rr := handlerRequest(fix, "/marketplaces/"+slug+target, "tenant-a")
				if rr.Code != tc.code {
					t.Fatalf("status %d, want %d: %s", rr.Code, tc.code, rr.Body.String())
				}
				if fix.crawler.fetchCalls != tc.crawls {
					t.Fatalf("crawls %d, want %d: %s", fix.crawler.fetchCalls, tc.crawls, rr.Body.String())
				}
				if len(fix.cache.setCalls) != tc.writes {
					t.Fatalf("writes %d, want %d: %s", len(fix.cache.setCalls), tc.writes, rr.Body.String())
				}
				if strings.Contains(rr.Body.String(), "private") {
					t.Fatal("underlying error leaked to response")
				}
			})
		}
	}
}

func TestControllerRequiresTenant(t *testing.T) {
	slug := handlerSource.MarketplaceSlug
	for _, target := range []string{
		"/marketplaces",
		"/marketplaces/" + slug + "/skills",
		"/marketplaces/" + slug + "/skills/" + slug + "__react-expert",
	} {
		for _, claims := range []*token.PlatformClaims{nil, {}} {
			fix := newHandlerController(true)
			ctx := token.WithClaims(context.Background(), claims)
			mux := api.NewMux(ctx, fix.logger)
			fix.Boot(mux)
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil))
			if rr.Code != http.StatusUnauthorized || len(fix.registry.calls) != 0 {
				t.Fatalf("missing tenant reached data access: %d %s", rr.Code, rr.Body.String())
			}
		}
	}
}

type handlerEvaluator struct {
	policy.Evaluator
	allow  bool
	err    error
	inputs []policy.CheckInput
}

func (f *handlerEvaluator) Check(_ context.Context, input policy.CheckInput) (policy.Result, error) {
	f.inputs = append(f.inputs, input)
	return policy.Result{Allow: f.allow}, f.err
}

func TestControllerAuthorization(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	logger := logging.NewDevelopmentLogger()
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.JSON(r.Context(), w, logger, http.StatusOK, map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "skills-test", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(jwks.Close)
	verifier := token.NewVerifier(context.Background(), jwks.URL, token.DefaultIssuer, token.DefaultAudience)
	claims := token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: token.DefaultIssuer, Audience: jwt.ClaimStrings{token.DefaultAudience},
			Subject: "caller", ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)),
		},
		Orbit: token.OrbitClaims{TenantID: "tenant-a", PrincipalType: token.PrincipalTypeHuman},
	}
	signed := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed.Header["kid"] = "skills-test"
	validToken, err := signed.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	delegationClaims := jwt.MapClaims{
		"iss": token.DefaultIssuer, "aud": token.DefaultAudience,
		"exp": time.Now().Add(5 * time.Minute).Unix(), "sub": "caller",
		"act": map[string]string{"sub": "agent-1"},
		"jti": "delegation-1", "run_id": "run-1",
		"delegator_principal_type": string(token.PrincipalTypeHuman),
		"tenant_id":                "tenant-a",
	}
	signedDelegation := jwt.NewWithClaims(jwt.SigningMethodRS256, delegationClaims)
	signedDelegation.Header["kid"] = "skills-test"
	validDelegation, err := signedDelegation.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}

	slug := handlerSource.MarketplaceSlug
	targets := []string{
		"/marketplaces",
		"/marketplaces/" + slug + "/skills",
		"/marketplaces/" + slug + "/skills/" + slug + "__react-expert",
	}

	for _, tc := range []struct {
		name, auth, delegation string
		allow                  bool
		policyErr              error
		code, checks           int
	}{
		{"missing token", "", "", true, nil, http.StatusUnauthorized, 0},
		{"invalid token", "invalid", "", true, nil, http.StatusUnauthorized, 0},
		{"invalid delegation", validToken, "invalid", true, nil, http.StatusUnauthorized, 0},
		{"valid delegation token", validToken, validDelegation, true, nil, http.StatusOK, 1},
		{"denied scope", validToken, "", false, nil, http.StatusForbidden, 1},
		{"policy unavailable", validToken, "", true, errors.New("policy unavailable"), http.StatusForbidden, 1},
		{"allowed", validToken, "", true, nil, http.StatusOK, 1},
	} {
		for _, target := range targets {
			t.Run(tc.name+target, func(t *testing.T) {
				fixture := newHandlerController(true)
				eval := &handlerEvaluator{allow: tc.allow, err: tc.policyErr}
				svc := NewService(fixture.registry, fixture.cache, fixture.crawler, logger)
				c := NewController(logger, svc, fixture.registry, verifier, eval, "/v1", "")
				server := api.NewAPI(context.Background(), logger).Server(context.Background(), 0, "/v1", nil, c)
				r := httptest.NewRequest(http.MethodGet, "/v1"+target, nil)
				if tc.auth != "" {
					r.Header.Set("Authorization", "Bearer "+tc.auth)
				}
				if tc.delegation != "" {
					r.Header.Set("X-Orbit-Delegation-Token", tc.delegation)
				}
				rr := httptest.NewRecorder()
				server.Handler.ServeHTTP(rr, r)
				if rr.Code != tc.code || len(eval.inputs) != tc.checks {
					t.Fatalf("status %d, policy calls %d: %s", rr.Code, len(eval.inputs), rr.Body.String())
				}
				if tc.code != http.StatusOK && (len(fixture.registry.calls) != 0 || len(fixture.cache.getCalls) != 0) {
					t.Fatal("denied request reached marketplace data")
				}
				if tc.checks == 1 {
					input := eval.inputs[0]
					if input.Claims.Orbit.TenantID != "tenant-a" || input.ResourceType != "marketplace" || input.Action != "read" {
						t.Fatalf("wrong policy input: %+v", input)
					}
					if tc.delegation != "" {
						if input.Delegation == nil || input.Delegation.JTI != "delegation-1" {
							t.Fatalf("delegation did not reach policy: %+v", input.Delegation)
						}
					} else if input.Delegation != nil {
						t.Fatalf("unexpected delegation in policy input: %+v", input.Delegation)
					}
				}
			})
		}
	}
}
