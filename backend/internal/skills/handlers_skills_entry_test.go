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
	"strconv"
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

type handlerPagination struct {
	Offset     int `json:"offset"`
	Limit      int `json:"limit"`
	TotalItems int `json:"totalItems"`
}

type handlerListResponse struct {
	Skills     []map[string]string `json:"skills"`
	Pagination handlerPagination   `json:"pagination"`
}

type handlerDetailResponse struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Skill       SkillFile   `json:"skill"`
	Documents   []SkillFile `json:"documents"`
}

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
	skills []Skill
	err    error
	calls  []MarketplaceSource
}

func (f *handlerCrawler) Crawl(_ context.Context, source MarketplaceSource) ([]Skill, error) {
	f.calls = append(f.calls, source)
	return f.skills, f.err
}

func handlerSkills() []Skill {
	return []Skill{
		{
			Slug: "internal-org/internal-repo/react-expert", Name: "React Expert", Description: "React development",
			Skill: SkillFile{Path: "plugins/web/skills/react-expert/SKILL.md", Name: "SKILL.md", Content: "private skill body"},
		},
		{
			Slug: "internal-org/internal-repo/react-testing", Name: "React Testing", Description: "React tests",
			Skill:     SkillFile{Path: "skills/react-testing/SKILL.md", Name: "SKILL.md", Content: "testing skill body"},
			Documents: []SkillFile{{Path: "skills/react-testing/resources/example.md", Name: "example.md", Content: "resource body"}},
		},
		{
			Slug: "internal-org/internal-repo/go-expert", Name: "Go Expert", Description: "Go development",
			Skill: SkillFile{Path: "skills/go-expert/SKILL.md", Name: "SKILL.md", Content: "go skill body"},
		},
	}
}

func newHandlerController(cached bool) *Controller {
	cache := &handlerCache{}
	if cached {
		cache.values = map[string][]Skill{CacheKey("tenant-a", handlerSource.Org, handlerSource.Repo): handlerSkills()}
	}
	return &Controller{
		registry: &handlerRegistry{sources: map[string][]MarketplaceSource{"tenant-a": {handlerSource}}},
		cache:    cache,
		crawler:  &handlerCrawler{skills: handlerSkills()},
		logger:   logging.NewDevelopmentLogger(),
	}
}

// Exercise routing and transport with verified claims supplied in context.
// TestControllerAuthorization separately exercises the full middleware chain.
func handlerRequest(c *Controller, target, tenantID string) *httptest.ResponseRecorder {
	ctx := token.WithClaims(context.Background(), &token.PlatformClaims{
		Orbit: token.OrbitClaims{TenantID: tenantID, PrincipalType: token.PrincipalTypeHuman},
	})
	mux := api.NewMux(ctx, c.logger)
	c.Boot(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil))
	return recorder
}

func TestControllerMarketplaces(t *testing.T) {
	for _, tenantID := range []string{"tenant-a", "tenant-b"} {
		t.Run(tenantID, func(t *testing.T) {
			c := newHandlerController(true)
			rr := handlerRequest(c, "/marketplaces?tenant_id=tenant-a", tenantID)
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
			if !reflect.DeepEqual(c.registry.(*handlerRegistry).calls, []string{tenantID}) {
				t.Fatal("request parameters changed tenant resolution")
			}
			if len(c.cache.(*handlerCache).getCalls) != 0 || len(c.crawler.(*handlerCrawler).calls) != 0 {
				t.Fatal("marketplace listing must not call Redis or GitHub")
			}
		})
	}
}

func TestControllerListSkills(t *testing.T) {
	c := newHandlerController(true)
	rr := handlerRequest(c, "/marketplaces/clear-route-skills/skills?search=REACT&offset=1&limit=1", "tenant-a")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rr.Code, rr.Body.String())
	}
	var body struct {
		Skills     []map[string]any  `json:"skills"`
		Pagination handlerPagination `json:"pagination"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	want := []map[string]any{{
		"slug": "internal-org/internal-repo/react-testing", "name": "React Testing", "description": "React tests",
	}}
	if !reflect.DeepEqual(body.Skills, want) || body.Pagination != (handlerPagination{Offset: 1, Limit: 1, TotalItems: 2}) {
		t.Fatalf("unexpected list response: %s", rr.Body.String())
	}
	if len(c.crawler.(*handlerCrawler).calls) != 0 {
		t.Fatal("cache hit caused a crawl")
	}
}

func TestControllerPagination(t *testing.T) {
	for _, tc := range []struct {
		query string
		code  int
		count int
	}{
		{"", http.StatusOK, 15},
		{"?limit=100", http.StatusOK, 20},
		{"?offset=20", http.StatusOK, 0},
		{"?offset=" + strconv.Itoa(int(^uint(0)>>1)), http.StatusOK, 0},
		{"?search=absent", http.StatusOK, 0},
		{"?offset=-1", http.StatusBadRequest, 0},
		{"?offset=no", http.StatusBadRequest, 0},
		{"?offset=999999999999999999999999", http.StatusBadRequest, 0},
		{"?limit=0", http.StatusBadRequest, 0},
		{"?limit=101", http.StatusBadRequest, 0},
		{"?limit=no", http.StatusBadRequest, 0},
		{"?offset=", http.StatusBadRequest, 0},
		{"?limit=", http.StatusBadRequest, 0},
	} {
		t.Run(tc.query, func(t *testing.T) {
			c := newHandlerController(true)
			items := make([]Skill, 20)
			c.cache.(*handlerCache).values[CacheKey("tenant-a", handlerSource.Org, handlerSource.Repo)] = items
			rr := handlerRequest(c, "/marketplaces/clear-route-skills/skills"+tc.query, "tenant-a")
			if rr.Code != tc.code {
				t.Fatalf("status %d, want %d: %s", rr.Code, tc.code, rr.Body.String())
			}
			if tc.code != http.StatusOK {
				if len(c.cache.(*handlerCache).getCalls) != 0 {
					t.Fatal("invalid query reached the cache")
				}
				return
			}
			var body handlerListResponse
			if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Skills == nil || len(body.Skills) != tc.count {
				t.Fatalf("got %v skills, want non-nil slice of length %d", body.Skills, tc.count)
			}
			if tc.query == "" && (body.Pagination.Offset != 0 || body.Pagination.Limit != 15 || body.Pagination.TotalItems != 20) {
				t.Fatalf("wrong pagination defaults: %+v", body.Pagination)
			}
		})
	}
}

func TestControllerCacheMissAndDetail(t *testing.T) {
	for _, first := range []string{"/skills", "/skills/react-expert"} {
		t.Run(first, func(t *testing.T) {
			c := newHandlerController(false)
			prefix := "/marketplaces/clear-route-skills"
			rr := handlerRequest(c, prefix+first+"?tenant_id=tenant-b", "tenant-a")
			if rr.Code != http.StatusOK {
				t.Fatalf("cold cache status %d: %s", rr.Code, rr.Body.String())
			}
			rr = handlerRequest(c, prefix+"/skills/react-expert", "tenant-a")
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
			cache := c.cache.(*handlerCache)
			key := CacheKey("tenant-a", handlerSource.Org, handlerSource.Repo)
			if !reflect.DeepEqual(cache.getCalls, []string{key, key}) || !reflect.DeepEqual(cache.setCalls, []string{key}) {
				t.Fatalf("cache calls: get %v, set %v", cache.getCalls, cache.setCalls)
			}
			if !reflect.DeepEqual(c.crawler.(*handlerCrawler).calls, []MarketplaceSource{handlerSource}) {
				t.Fatal("cold list/detail did not share one cache fill")
			}
			rr = handlerRequest(c, prefix+"/skills/react-testing", "tenant-a")
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
			c := newHandlerController(true)
			if tc.name == "ambiguous marketplace" {
				c.registry.(*handlerRegistry).sources["tenant-a"] = append(
					c.registry.(*handlerRegistry).sources["tenant-a"],
					MarketplaceSource{Name: "Clear-Route-Skills", Org: "other", Repo: "other"},
				)
			}
			rr := handlerRequest(c, tc.target, tc.tenantID)
			if rr.Code != tc.code || len(c.cache.(*handlerCache).getCalls) != tc.reads {
				t.Fatalf("status %d, reads %v: %s", rr.Code, c.cache.(*handlerCache).getCalls, rr.Body.String())
			}
			if len(c.crawler.(*handlerCrawler).calls) != 0 {
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
				c := newHandlerController(false)
				cache := c.cache.(*handlerCache)
				cache.getError, cache.setErr = tc.getErr, tc.setErr
				crawler := c.crawler.(*handlerCrawler)
				crawler.err = tc.crawlErr
				rr := handlerRequest(c, "/marketplaces/clear-route-skills"+target, "tenant-a")
				if rr.Code != tc.code || len(crawler.calls) != tc.crawls || len(cache.setCalls) != tc.writes {
					t.Fatalf("status %d, crawls %d, writes %d: %s", rr.Code, len(crawler.calls), len(cache.setCalls), rr.Body.String())
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
			c := newHandlerController(true)
			ctx := token.WithClaims(context.Background(), claims)
			mux := api.NewMux(ctx, c.logger)
			c.Boot(mux)
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil))
			if rr.Code != http.StatusUnauthorized || len(c.registry.(*handlerRegistry).calls) != 0 {
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

	for _, tc := range []struct {
		name, auth, delegation string
		allow                  bool
		policyErr              error
		code, checks           int
	}{
		{"missing token", "", "", true, nil, http.StatusUnauthorized, 0},
		{"invalid token", "invalid", "", true, nil, http.StatusUnauthorized, 0},
		{"invalid delegation", validToken, "invalid", true, nil, http.StatusUnauthorized, 0},
		{"denied scope", validToken, "", false, nil, http.StatusForbidden, 1},
		{"policy unavailable", validToken, "", true, errors.New("policy unavailable"), http.StatusForbidden, 1},
		{"allowed", validToken, "", true, nil, http.StatusOK, 1},
	} {
		for _, target := range []string{"/marketplaces", "/marketplaces/clear-route-skills/skills", "/marketplaces/clear-route-skills/skills/react-expert"} {
			t.Run(tc.name+target, func(t *testing.T) {
				fixture := newHandlerController(true)
				eval := &handlerEvaluator{allow: tc.allow, err: tc.policyErr}
				c := NewController(logger, fixture.registry, fixture.cache, fixture.crawler, verifier, eval, "/v1")
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
				if tc.code != http.StatusOK && (len(fixture.registry.(*handlerRegistry).calls) != 0 || len(fixture.cache.(*handlerCache).getCalls) != 0) {
					t.Fatal("denied request reached marketplace data")
				}
				if tc.checks == 1 {
					input := eval.inputs[0]
					if input.Claims.Orbit.TenantID != "tenant-a" || input.ResourceType != "marketplace" || input.Action != "read" {
						t.Fatalf("wrong policy input: %+v", input)
					}
				}
			})
		}
	}
}
