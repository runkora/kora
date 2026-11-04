package skills

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/runkora/kora/backend/internal/platform/api"
	"github.com/runkora/kora/backend/internal/platform/policy"
	"github.com/runkora/kora/backend/internal/platform/storage"
	"github.com/runkora/kora/backend/internal/platform/token"
)

type fakeEvaluator struct {
	policy.Evaluator
	allow  bool
	inputs []policy.CheckInput
}

func (f *fakeEvaluator) Check(_ context.Context, input policy.CheckInput) (policy.Result, error) {
	f.inputs = append(f.inputs, input)
	return policy.Result{Allow: f.allow}, nil
}

type noBlocklist struct{}

func (noBlocklist) IsBlocked(context.Context, string) (bool, error) { return false, nil }

// apiFixture serves the controller through the real auth and scope chain.
type apiFixture struct {
	t       *testing.T
	handler http.Handler
	eval    *fakeEvaluator
	token   string
	github  *fakeGitHub
}

func newAPIFixture(t *testing.T, allow bool, ms ...*Marketplace) *apiFixture {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	logger := discard()
	jwks := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		api.JSON(r.Context(), w, logger, http.StatusOK, map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": "skills-test", "use": "sig", "alg": "RS256",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(jwks.Close)
	signed := jwt.NewWithClaims(jwt.SigningMethodRS256, token.PlatformClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer: token.DefaultIssuer, Audience: jwt.ClaimStrings{token.DefaultAudience},
			Subject: "caller", ExpiresAt: jwt.NewNumericDate(time.Now().Add(5 * time.Minute)), ID: "platform-1",
		},
		Kora: token.KoraClaims{TenantID: "example", PrincipalType: token.PrincipalTypeHuman},
	})
	signed.Header["kid"] = "skills-test"
	bearer, err := signed.SignedString(key)
	if err != nil {
		t.Fatal(err)
	}

	g := sampleRepo()
	svc, _, _ := newTestService(t, g, ms...)
	eval := &fakeEvaluator{allow: allow}
	verifier := token.NewVerifier(context.Background(), jwks.URL, token.DefaultIssuer, token.DefaultAudience)
	c := NewController(logger, svc, verifier, eval, noBlocklist{}, "/v1")
	server := api.NewAPI(context.Background(), logger).Server(context.Background(), 0, "/v1", nil, c)
	return &apiFixture{t: t, handler: server.Handler, eval: eval, token: bearer, github: g}
}

func (f *apiFixture) get(target string, out any) int {
	f.t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/v1"+target, nil)
	r.Header.Set("Authorization", "Bearer "+f.token)
	rr := httptest.NewRecorder()
	f.handler.ServeHTTP(rr, r)
	if out != nil {
		if err := json.Unmarshal(rr.Body.Bytes(), out); err != nil {
			f.t.Fatalf("decode %s: %v: %s", target, err, rr.Body.String())
		}
	}
	return rr.Code
}

func TestAPI_Contract(t *testing.T) {
	f := newAPIFixture(t, true, testMarketplace(t, "example", "team-skills"))

	var list ListMarketplacesResponse
	if code := f.get("/marketplaces", &list); code != http.StatusOK || len(list.Marketplaces) != 1 || list.Marketplaces[0].Commit != nil || list.Marketplaces[0].SkillCount != nil {
		t.Errorf("GET /marketplaces before a crawl = %d %+v, want null commit and count", code, list)
	}
	if fetched := f.github.fetchedPaths(); len(fetched) != 0 {
		t.Errorf("listing Marketplaces crawled: %v", fetched)
	}

	var page SkillPage
	if code := f.get("/skills?marketplace=team-skills&search=pdf&offset=0&limit=1", &page); code != http.StatusOK || page.Total != 2 || page.Limit != 1 || len(page.Skills) != 1 || page.Skills[0].Path != "skills/pdf" || page.Skills[0].Marketplace != "team-skills" {
		t.Errorf("GET /skills = %d %+v", code, page)
	}
	if code := f.get("/skills?limit=500", &page); code != http.StatusOK || page.Limit != MaxLimit {
		t.Errorf("limit 500 = %d limit %d, want capped at %d", code, page.Limit, MaxLimit)
	}
	if code := f.get("/skills", &page); page.Limit != DefaultLimit || page.Offset != 0 || code != http.StatusOK || page.Total != 6 {
		t.Errorf("defaults = %d %+v", code, page)
	}
	if code := f.get("/skills?keys=team-skills:skills/pdf,team-skills:skills/missing,other:skills/pdf", &page); code != http.StatusOK || page.Total != 1 || page.Skills[0].Path != "skills/pdf" {
		t.Errorf("keys = %d %+v, want only the known key", code, page)
	}

	var raw map[string]any
	if code := f.get("/marketplaces", &raw); code != http.StatusOK {
		t.Fatalf("GET /marketplaces = %d", code)
	}
	after := raw["marketplaces"].([]any)[0].(map[string]any)
	if after["commit"] != testSHA || after["skillCount"] != float64(6) || after["syncedAt"] == nil || after["ref"] != "" {
		t.Errorf("GET /marketplaces after a crawl = %+v", after)
	}

	var skill map[string]any
	if code := f.get("/marketplaces/team-skills/skills/skills/pdf", &skill); code != http.StatusOK || skill["path"] != "skills/pdf" || skill["content"] == "" ||
		skill["marketplace"] != "team-skills" || skill["title"] != "pdf" || skill["commit"] != testSHA || skill["ref"] != "" {
		t.Errorf("GET skill = %d %+v", code, skill)
	}
	if files, _ := skill["files"].([]any); len(files) != 1 || !reflect.DeepEqual(files[0], map[string]any{"path": "forms.md", "size": float64(5)}) {
		t.Errorf("files = %v, want one entry with path and size only", skill["files"])
	}
	var file SkillFileContent
	if code := f.get("/marketplaces/team-skills/skills/skills/pdf?file=forms.md", &file); code != http.StatusOK || file != (SkillFileContent{Path: "forms.md", Content: "forms"}) {
		t.Errorf("GET ?file= = %d %+v", code, file)
	}

	if len(f.eval.inputs) == 0 || f.eval.inputs[0].Claims.Kora.TenantID != "example" {
		t.Errorf("policy inputs = %+v", f.eval.inputs)
	}
}

func TestAPI_Errors(t *testing.T) {
	noKey := testMarketplace(t, "example", "no-key")
	noKey.tokens = nil
	f := newAPIFixture(t, true, testMarketplace(t, "example", "team-skills"), noKey)

	for _, tc := range []struct {
		target string
		code   int
		error  string
	}{
		{"/skills?offset=-1", http.StatusBadRequest, codeInvalidRequest},
		{"/skills?limit=0", http.StatusBadRequest, codeInvalidRequest},
		{"/skills?limit=ten", http.StatusBadRequest, codeInvalidRequest},
		{"/skills?marketplace=unknown", http.StatusNotFound, codeNotFound},
		{"/marketplaces/team-skills/skills/skills/missing", http.StatusNotFound, codeNotFound},
		{"/marketplaces/team-skills/skills/skills/pdf?file=unlisted.md", http.StatusNotFound, codeNotFound},
		{"/marketplaces/team-skills/skills/skills/pdf?file=../docx/SKILL.md", http.StatusNotFound, codeNotFound},
		{"/marketplaces/team-skills/skills/skills/pdf?file=", http.StatusNotFound, codeNotFound},
		// A Marketplace listed without its key fails closed.
		{"/skills?marketplace=no-key", http.StatusBadGateway, codeMarketplaceUnavailable},
		{"/marketplaces/no-key/skills/skills/pdf", http.StatusBadGateway, codeMarketplaceUnavailable},
	} {
		var body api.APIError
		if code := f.get(tc.target, &body); code != tc.code || body.Code != tc.error || body.Message == "" {
			t.Errorf("%s = %d %+v, want %d %s", tc.target, code, body, tc.code, tc.error)
		}
	}
}

// Across Marketplaces a failed one is listed in failures and the others are
// served.
func TestAPI_AllMarketplacesReportFailures(t *testing.T) {
	noKey := testMarketplace(t, "example", "no-key")
	noKey.tokens = nil
	f := newAPIFixture(t, true, testMarketplace(t, "example", "team-skills"), noKey)

	var page SkillPage
	if code := f.get("/skills", &page); code != http.StatusOK || page.Total != 6 {
		t.Fatalf("GET /skills = %d %+v", code, page)
	}
	want := []MarketplaceFailure{{Marketplace: "no-key", Message: "The credential for this Marketplace is unavailable."}}
	if !slices.Equal(page.Failures, want) {
		t.Errorf("failures = %+v, want %+v", page.Failures, want)
	}

	var raw map[string]any
	f2 := newAPIFixture(t, true, testMarketplace(t, "example", "team-skills"))
	f2.get("/skills", &raw)
	if _, ok := raw["failures"]; ok {
		t.Errorf("failures present with none: %v", raw)
	}
}

// A path with an empty, "." or ".." segment is refused before any lookup, so
// no client path reaches GitHub. The mux redirects most of them to the cleaned
// path first; the handler refuses whatever reaches it.
func TestAPI_SkillPathSegments(t *testing.T) {
	f := newAPIFixture(t, true, testMarketplace(t, "example", "team-skills"))
	for _, target := range []string{
		"/marketplaces/team-skills/skills/skills//pdf",
		"/marketplaces/team-skills/skills/skills/./pdf",
		"/marketplaces/team-skills/skills/skills/%2E%2E/pdf",
		"/marketplaces/team-skills/skills/skills/pdf/",
	} {
		if code := f.get(target, nil); code != http.StatusNotFound && code != http.StatusTemporaryRedirect {
			t.Errorf("%s = %d, want 404 or the mux's redirect", target, code)
		}
	}
	if fetched := f.github.fetchedPaths(); len(fetched) != 0 {
		t.Errorf("GitHub fetched %v, want nothing", fetched)
	}

	svc, crawler, _ := newTestService(t, sampleRepo(), testMarketplace(t, "example", "team-skills"))
	c := &Controller{service: svc, logger: discard()}
	ctx := token.WithClaims(context.Background(), &token.PlatformClaims{Kora: token.KoraClaims{TenantID: "example"}})
	for _, p := range []string{"", "skills//pdf", "skills/./pdf", "skills/../pdf", "../skills/pdf", "skills/pdf/"} {
		r := httptest.NewRequestWithContext(ctx, http.MethodGet, "/v1/marketplaces/team-skills/skills/x", nil)
		r.SetPathValue("slug", "team-skills")
		r.SetPathValue("path", p)
		rr := httptest.NewRecorder()
		c.handleGetSkill(rr, r)
		var body api.APIError
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if rr.Code != http.StatusNotFound || body.Code != codeNotFound {
			t.Errorf("path %q = %d %+v, want 404 not_found", p, rr.Code, body)
		}
	}
	if crawler.crawls != 0 {
		t.Errorf("crawls = %d, want none for a refused path", crawler.crawls)
	}
}

func TestAPI_DeniedScope(t *testing.T) {
	f := newAPIFixture(t, false, testMarketplace(t, "example", "team-skills"))
	if code := f.get("/skills", nil); code != http.StatusForbidden {
		t.Errorf("denied scope = %d, want 403", code)
	}
	input := f.eval.inputs[0]
	if input.ResourceType != "skills" || input.Action != "read" {
		t.Errorf("policy input = %s:%s, want skills:read", input.ResourceType, input.Action)
	}
	if len(f.github.fetchedPaths()) != 0 {
		t.Error("a denied request reached GitHub")
	}

	r := httptest.NewRequest(http.MethodGet, "/v1/marketplaces", nil)
	rr := httptest.NewRecorder()
	f.handler.ServeHTTP(rr, r)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("no token = %d, want 401", rr.Code)
	}
}

func TestRespondError(t *testing.T) {
	c := &Controller{logger: discard()}
	for _, tc := range []struct {
		err  error
		code int
		body string
	}{
		{fmt.Errorf("cache get: %w", storage.ErrRedisUnavailable), http.StatusServiceUnavailable, codeUnavailable},
		{fmt.Errorf("crawl: %w", context.DeadlineExceeded), http.StatusGatewayTimeout, codeTimeout},
		{ErrGitHubFailure, http.StatusBadGateway, codeMarketplaceUnavailable},
		{ErrCredentialUnavailable, http.StatusBadGateway, codeMarketplaceUnavailable},
		{ErrRepositoryNotFound, http.StatusNotFound, codeNotFound},
		{ErrFileNotFound, http.StatusNotFound, codeNotFound},
	} {
		rr := httptest.NewRecorder()
		c.respondError(rr, httptest.NewRequest(http.MethodGet, "/", nil), tc.err)
		var body api.APIError
		_ = json.Unmarshal(rr.Body.Bytes(), &body)
		if rr.Code != tc.code || body.Code != tc.body {
			t.Errorf("%v = %d %s, want %d %s", tc.err, rr.Code, body.Code, tc.code, tc.body)
		}
		if strings.Contains(body.Message, "GitHub") {
			t.Errorf("%v message %q names the provider", tc.err, body.Message)
		}
	}
}

// A caller that went away gets no response.
func TestRespondError_CancelledWritesNothing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rr := httptest.NewRecorder()
	(&Controller{logger: discard()}).respondError(rr, httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil), context.Canceled)
	if rr.Body.Len() != 0 || rr.Result().Header.Get("Content-Type") != "" {
		t.Errorf("cancelled request wrote %d %q", rr.Code, rr.Body.String())
	}
}

// keys past MaxKeys are dropped; the request still succeeds.
func TestAPI_KeysAreCapped(t *testing.T) {
	f := newAPIFixture(t, true, testMarketplace(t, "example", "team-skills"))
	keys := make([]string, 0, MaxKeys+1)
	for i := range MaxKeys {
		keys = append(keys, fmt.Sprintf("team-skills:skills/none-%d", i))
	}
	keys = append(keys, "team-skills:skills/pdf")
	var page SkillPage
	if code := f.get("/skills?keys="+strings.Join(keys, ","), &page); code != http.StatusOK || page.Total != 0 {
		t.Errorf("GET /skills with %d keys = %d total %d, want 200 and the key past the cap ignored", len(keys), code, page.Total)
	}
}

func TestRoutes(t *testing.T) {
	var got []string
	for _, r := range Routes("/v1") {
		got = append(got, r.Method+" "+r.PathPrefix+" "+r.RequiredScope)
	}
	if want := []string{"GET /v1/marketplaces skills:read", "GET /v1/skills skills:read"}; !slices.Equal(got, want) {
		t.Errorf("routes = %v, want %v", got, want)
	}
}
