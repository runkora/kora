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
	"reflect"
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
	Name: "Clear Route Skills", Org: "internal-org", Repo: "internal-repo", Branch: "main",
}

type handlerListResponse struct {
	Skills []map[string]string `json:"skills"`
}

type handlerDetailResponse struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Skill       SkillFile   `json:"skill"`
	Documents   []SkillFile `json:"documents"`
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
	values           map[string][]Skill
	getCalls         []string
	setCalls         []string
	getError, setErr error
}

func (f *handlerCache) Get(_ context.Context, tenantID, org, repo string) ([]Skill, bool, error) {
	key := CacheKey(tenantID, org, repo)
	f.getCalls = append(f.getCalls, key)
	if f.getError != nil {
		return nil, false, f.getError
	}
	skills, hit := f.values[key]
	return skills, hit, nil
}

func (f *handlerCache) Set(_ context.Context, tenantID, org, repo string, skills []Skill) error {
	key := CacheKey(tenantID, org, repo)
	f.setCalls = append(f.setCalls, key)
	if f.setErr != nil {
		return f.setErr
	}
	if f.values == nil {
		f.values = make(map[string][]Skill)
	}
	f.values[key] = skills
	return nil
}

type handlerCrawler struct {
	skills    []Skill
	skillsErr error
	calls     int
}

func (f *handlerCrawler) FetchTree(_ context.Context, _ MarketplaceSource) (map[string]SkillFilePaths, error) {
	return map[string]SkillFilePaths{}, nil
}

func (f *handlerCrawler) FetchSkills(_ context.Context, _ string, _ MarketplaceSource, _ map[string]SkillFilePaths) ([]Skill, error) {
	f.calls++
	return f.skills, f.skillsErr
}

// handlerFixture bundles a Controller with the underlying fakes so tests can
// inspect cache and crawl calls without the Controller exposing those fields.
type handlerFixture struct {
	*Controller
	registry *handlerRegistry
	cache    *handlerCache
	crawler  *handlerCrawler
}

func handlerSkills() []Skill {
	return []Skill{
		{
			Slug:        "internal-org:internal-repo:react-expert",
			Name:        "React Expert",
			Description: "React development",
			Skill:       SkillFile{Path: "plugins/web/skills/react-expert/SKILL.md", Name: "SKILL.md", Content: "private skill body"},
			Documents:   []SkillFile{},
		},
		{
			Slug:        "internal-org:internal-repo:react-testing",
			Name:        "React Testing",
			Description: "React tests",
			Skill:       SkillFile{Path: "skills/react-testing/SKILL.md", Name: "SKILL.md", Content: "testing skill body"},
			Documents:   []SkillFile{{Path: "skills/react-testing/resources/example.md", Name: "example.md", Content: "resource body"}},
		},
		{
			Slug:        "internal-org:internal-repo:go-expert",
			Name:        "Go Expert",
			Description: "Go development",
			Skill:       SkillFile{Path: "skills/go-expert/SKILL.md", Name: "SKILL.md", Content: "go skill body"},
			Documents:   []SkillFile{},
		},
	}
}

func newHandlerController(cached bool) *handlerFixture {
	reg := &handlerRegistry{sources: map[string][]MarketplaceSource{"tenant-a": {handlerSource}}}
	cache := &handlerCache{}
	if cached {
		cache.values = map[string][]Skill{CacheKey("tenant-a", handlerSource.Org, handlerSource.Repo): handlerSkills()}
	}
	crawler := &handlerCrawler{skills: handlerSkills()}
	logger := logging.NewDevelopmentLogger()
	svc := NewService(reg, cache, crawler, logger)
	c := &Controller{service: svc, registry: reg, logger: logger}
	return &handlerFixture{Controller: c, registry: reg, cache: cache, crawler: crawler}
}

// Exercise routing and transport with verified claims supplied in context.
// TestControllerAuthorization separately exercises the full middleware chain.
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
			rr := handlerRequest(fix, "/marketplaces?tenant_id=tenant-a", tenantID)
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
				want := []map[string]any{{"name": "Clear Route Skills"}}
				if !reflect.DeepEqual(body.Marketplaces, want) {
					t.Fatalf("marketplaces = %v, want names only", body.Marketplaces)
				}
			} else if body.Marketplaces == nil || len(body.Marketplaces) != 0 {
				t.Fatalf("unknown tenant must get [], got %v", body.Marketplaces)
			}
			if !reflect.DeepEqual(fix.registry.calls, []string{tenantID}) {
				t.Fatal("request parameters changed tenant resolution")
			}
			if len(fix.cache.getCalls) != 0 || fix.crawler.calls != 0 {
				t.Fatal("marketplace listing must not call Redis or GitHub")
			}
		})
	}
}

func TestControllerListSkills(t *testing.T) {
	fix := newHandlerController(true)
	rr := handlerRequest(fix, "/marketplaces/clear-route-skills/skills?search=REACT", "tenant-a")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Skills []map[string]any `json:"skills"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	// List response exposes only name and description — no slug.
	want := []map[string]any{
		{"name": "React Expert", "description": "React development"},
		{"name": "React Testing", "description": "React tests"},
	}
	if !reflect.DeepEqual(body.Skills, want) {
		t.Fatalf("unexpected list response: %s", rr.Body.String())
	}
	if fix.crawler.calls != 0 {
		t.Fatal("cache hit caused a crawl")
	}
}

func TestControllerServerCap(t *testing.T) {
	fix := newHandlerController(true)
	items := make([]Skill, serverCap+10)
	fix.cache.values[CacheKey("tenant-a", handlerSource.Org, handlerSource.Repo)] = items
	rr := handlerRequest(fix, "/marketplaces/clear-route-skills/skills", "tenant-a")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var body handlerListResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Skills) != serverCap {
		t.Fatalf("got %d skills, want serverCap %d", len(body.Skills), serverCap)
	}
}

func TestControllerCacheMissAndDetail(t *testing.T) {
	for _, first := range []string{"/skills", "/skills/react-expert"} {
		t.Run(first, func(t *testing.T) {
			fix := newHandlerController(false)
			prefix := "/marketplaces/clear-route-skills"
			rr := handlerRequest(fix, prefix+first+"?tenant_id=tenant-b", "tenant-a")
			if rr.Code != http.StatusOK {
				t.Fatalf("cold cache status %d: %s", rr.Code, rr.Body.String())
			}
			rr = handlerRequest(fix, prefix+"/skills/react-expert", "tenant-a")
			if rr.Code != http.StatusOK {
				t.Fatalf("detail status %d: %s", rr.Code, rr.Body.String())
			}
			var body map[string]json.RawMessage
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if len(body) != 4 || string(body["documents"]) != "[]" || body["slug"] != nil {
				t.Fatalf("wrong detail fields: %s", rr.Body.String())
			}
			var detail handlerDetailResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &detail); err != nil {
				t.Fatal(err)
			}
			if detail.Skill != handlerSkills()[0].Skill || detail.Name != "React Expert" {
				t.Fatalf("detail lost SKILL.md content: %+v", detail)
			}
			key := CacheKey("tenant-a", handlerSource.Org, handlerSource.Repo)
			if !reflect.DeepEqual(fix.cache.getCalls, []string{key, key}) || !reflect.DeepEqual(fix.cache.setCalls, []string{key}) {
				t.Fatalf("cache calls: get %v, set %v", fix.cache.getCalls, fix.cache.setCalls)
			}
			if fix.crawler.calls != 1 {
				t.Fatal("cold list/detail did not share one cache fill")
			}
			rr = handlerRequest(fix, prefix+"/skills/react-testing", "tenant-a")
			if err := json.Unmarshal(rr.Body.Bytes(), &detail); err != nil {
				t.Fatal(err)
			}
			if rr.Code != http.StatusOK || !reflect.DeepEqual(detail.Documents, handlerSkills()[1].Documents) {
				t.Fatalf("detail lost resources: %s", rr.Body.String())
			}
		})
	}
}

func TestControllerNotFound(t *testing.T) {
	for _, tc := range []struct {
		name     string
		target   string
		tenantID string
		code     int
		reads    int
	}{
		{"unknown marketplace", "/marketplaces/unknown/skills", "tenant-a", http.StatusNotFound, 0},
		{"another tenant", "/marketplaces/clear-route-skills/skills?tenant_id=tenant-a", "tenant-b", http.StatusNotFound, 0},
		{"unknown skill", "/marketplaces/clear-route-skills/skills/missing", "tenant-a", http.StatusNotFound, 1},
		{"slash in skill name", "/marketplaces/clear-route-skills/skills/bad%2Fname", "tenant-a", http.StatusBadRequest, 0},
		{"ambiguous marketplace", "/marketplaces/clear-route-skills/skills", "tenant-a", http.StatusNotFound, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fix := newHandlerController(true)
			if tc.name == "ambiguous marketplace" {
				fix.registry.sources["tenant-a"] = append(
					fix.registry.sources["tenant-a"],
					MarketplaceSource{Name: "Clear-Route-Skills", Org: "other", Repo: "other"},
				)
			}
			rr := handlerRequest(fix, tc.target, tc.tenantID)
			if rr.Code != tc.code || len(fix.cache.getCalls) != tc.reads {
				t.Fatalf("status %d, reads %v: %s", rr.Code, fix.cache.getCalls, rr.Body.String())
			}
			if fix.crawler.calls != 0 {
				t.Fatal("not-found request caused a crawl")
			}
		})
	}
}

func TestControllerReadFailures(t *testing.T) {
	for _, target := range []string{"/skills", "/skills/react-expert"} {
		for _, tc := range []struct {
			name                     string
			getErr, crawlErr, setErr error
			code, crawls, writes     int
		}{
			{"Redis read", fmt.Errorf("private connection: %w", ErrRedisUnavailable), nil, nil, http.StatusServiceUnavailable, 0, 0},
			{"GitHub outage", nil, fmt.Errorf("private token: %w", ghclient.ErrUnavailable), nil, http.StatusServiceUnavailable, 1, 0},
			{"Redis write", nil, nil, ErrRedisUnavailable, http.StatusServiceUnavailable, 1, 1},
			{"corrupt cache", errors.New("private cached JSON"), nil, nil, http.StatusInternalServerError, 0, 0},
			{"unexpected crawl error", nil, errors.New("private configuration"), nil, http.StatusInternalServerError, 1, 0},
			{"crawl timeout", nil, context.DeadlineExceeded, nil, http.StatusServiceUnavailable, 1, 0},
		} {
			t.Run(target+"/"+tc.name, func(t *testing.T) {
				fix := newHandlerController(false)
				fix.cache.getError, fix.cache.setErr = tc.getErr, tc.setErr
				fix.crawler.skillsErr = tc.crawlErr
				rr := handlerRequest(fix, "/marketplaces/clear-route-skills"+target, "tenant-a")
				if rr.Code != tc.code || fix.crawler.calls != tc.crawls || len(fix.cache.setCalls) != tc.writes {
					t.Fatalf("status %d, crawls %d, writes %d: %s", rr.Code, fix.crawler.calls, len(fix.cache.setCalls), rr.Body.String())
				}
				if strings.Contains(rr.Body.String(), "private") {
					t.Fatal("underlying error leaked to response")
				}
			})
		}
	}
}

func TestControllerRequiresTenant(t *testing.T) {
	for _, target := range []string{"/marketplaces", "/marketplaces/clear-route-skills/skills", "/marketplaces/clear-route-skills/skills/react-expert"} {
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
	policy.Evaluator // Only Check is exercised by scope middleware.
	allow            bool
	err              error
	inputs           []policy.CheckInput
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
		"iss":                      token.DefaultIssuer,
		"aud":                      token.DefaultAudience,
		"exp":                      time.Now().Add(5 * time.Minute).Unix(),
		"sub":                      "caller",
		"act":                      map[string]string{"sub": "agent-1"},
		"jti":                      "delegation-1",
		"run_id":                   "run-1",
		"delegator_principal_type": string(token.PrincipalTypeHuman),
		"tenant_id":                "tenant-a",
	}
	signedDelegation := jwt.NewWithClaims(jwt.SigningMethodRS256, delegationClaims)
	signedDelegation.Header["kid"] = "skills-test"
	validDelegation, err := signedDelegation.SignedString(key)
	if err != nil {
		t.Fatal(err)
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
		for _, target := range []string{"/marketplaces", "/marketplaces/clear-route-skills/skills", "/marketplaces/clear-route-skills/skills/react-expert"} {
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
