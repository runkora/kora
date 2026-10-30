package skills

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

type searchCall struct {
	tenant, marketplace, query, cursor string
}

type searchReplacement struct {
	tenant, marketplace string
	skills              []Skill
}

type fakeSearch struct {
	searchFn     func(searchCall) (ListSkillsResponse, error)
	replaceErr   error
	calls        []searchCall
	replacements []searchReplacement
}

func (f *fakeSearch) Search(_ context.Context, tenant, marketplace, query, cursor string) (ListSkillsResponse, error) {
	call := searchCall{tenant, marketplace, query, cursor}
	f.calls = append(f.calls, call)
	if f.searchFn != nil {
		return f.searchFn(call)
	}
	return ListSkillsResponse{Skills: []SkillSummary{}}, nil
}

func (f *fakeSearch) ReplaceMarketplace(_ context.Context, tenant, marketplace string, skills []Skill) error {
	f.replacements = append(f.replacements, searchReplacement{tenant, marketplace, skills})
	return f.replaceErr
}

func TestSearchMarketplace_RebuildsFromFullCache(t *testing.T) {
	skills := makeSkills("authentication")
	cache := &fakeCache{
		marketplace: map[string][]SkillEntry{"org__repo": makeEntries("authentication")},
		skills:      map[string]Skill{skills[0].SkillSlug: skills[0]},
	}
	svc := newTestService(cache, &fakeCrawler{treeErr: errors.New("must not crawl")})
	search := &fakeSearch{}
	search.searchFn = func(searchCall) (ListSkillsResponse, error) {
		if len(search.replacements) == 0 {
			return ListSkillsResponse{}, ErrSearchIndexMissing
		}
		return ListSkillsResponse{Skills: []SkillSummary{{Name: "Authentication"}}}, nil
	}
	svc.search = search

	got, err := svc.ListSkills(context.Background(), "tenant-a", "org__repo", "token", "")
	if err != nil || len(got.Skills) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	if len(search.replacements) != 1 || !reflect.DeepEqual(search.replacements[0].skills, skills) {
		t.Fatalf("complete skill body was not indexed: %+v", search.replacements)
	}
}

func TestSearchMarketplace_MissingBodyCrawlsOnce(t *testing.T) {
	cache := &fakeCache{marketplace: map[string][]SkillEntry{"org__repo": makeEntries("authentication")}}
	skills := makeSkills("authentication")
	svc := newTestService(cache, &fakeCrawler{skills: skills})
	search := &fakeSearch{}
	search.searchFn = func(searchCall) (ListSkillsResponse, error) {
		if len(search.replacements) == 0 {
			return ListSkillsResponse{}, ErrSearchIndexMissing
		}
		return ListSkillsResponse{Skills: []SkillSummary{{Name: "Authentication"}}}, nil
	}
	svc.search = search

	_, err := svc.ListSkills(context.Background(), "tenant-a", "org__repo", "token", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(search.replacements) != 1 || !reflect.DeepEqual(search.replacements[0].skills, skills) {
		t.Fatalf("expected one full crawl/index, got %+v", search.replacements)
	}
}

func TestSearchMarketplace_EmptyCacheIsReady(t *testing.T) {
	cache := &fakeCache{marketplace: map[string][]SkillEntry{"org__repo": {}}}
	svc := newTestService(cache, &fakeCrawler{treeErr: errors.New("must not crawl")})
	search := &fakeSearch{}
	search.searchFn = func(searchCall) (ListSkillsResponse, error) {
		if len(search.replacements) == 0 {
			return ListSkillsResponse{}, ErrSearchIndexMissing
		}
		return ListSkillsResponse{Skills: []SkillSummary{}}, nil
	}
	svc.search = search
	got, err := svc.ListSkills(context.Background(), "tenant-a", "org__repo", "token", "")
	if err != nil || len(got.Skills) != 0 || len(search.replacements) != 1 {
		t.Fatalf("empty marketplace: %+v, %v, replacements %d", got, err, len(search.replacements))
	}
}

func TestSearchMarketplace_AccessCheckedBeforeSearch(t *testing.T) {
	svc := newTestService(&fakeCache{}, &fakeCrawler{})
	search := &fakeSearch{}
	svc.search = search
	for _, request := range []struct{ tenant, marketplace string }{
		{"tenant-b", "org__repo"}, {"tenant-a", "other__repo"},
	} {
		_, err := svc.ListSkills(context.Background(), request.tenant, request.marketplace, "token", "cursor")
		if !errors.Is(err, ErrMarketplaceNotFound) {
			t.Fatalf("got %v, want marketplace access rejection", err)
		}
	}
	if len(search.calls) != 0 {
		t.Fatal("search executed before marketplace access check")
	}
}

func TestSearchMarketplace_OutageDoesNotBecomeNoMatches(t *testing.T) {
	svc := newTestService(&fakeCache{}, &fakeCrawler{})
	svc.search = &fakeSearch{searchFn: func(searchCall) (ListSkillsResponse, error) {
		return ListSkillsResponse{}, ErrSearchUnavailable
	}}
	_, err := svc.ListSkills(context.Background(), "tenant-a", "org__repo", "token", "")
	if !errors.Is(err, ErrSearchUnavailable) {
		t.Fatalf("got %v, want search outage", err)
	}
}

func TestBrowse_SearchOutageDoesNotBreakRefresh(t *testing.T) {
	skills := makeSkills("authentication")
	svc := newTestService(&fakeCache{}, &fakeCrawler{skills: skills})
	search := &fakeSearch{replaceErr: ErrSearchUnavailable}
	svc.search = search
	got, err := svc.ListSkills(context.Background(), "tenant-a", "org__repo", "  ", "")
	if err != nil || len(got.Skills) != 1 || len(search.calls) != 0 {
		t.Fatalf("browsing depended on searching: %+v, %v", got, err)
	}
	if len(search.replacements) != 1 || !reflect.DeepEqual(search.replacements[0].skills, skills) {
		t.Fatal("list crawl did not pass full records to indexing")
	}
}

func TestGetSkill_RefreshIndexesCompleteMarketplace(t *testing.T) {
	skills := makeSkills("authentication", "authorization")
	svc := newTestService(&fakeCache{}, &fakeCrawler{skills: skills})
	search := &fakeSearch{}
	svc.search = search
	_, err := svc.GetSkill(context.Background(), "tenant-a", "org__repo", "authentication")
	if err != nil {
		t.Fatal(err)
	}
	if len(search.replacements) != 1 || !reflect.DeepEqual(search.replacements[0].skills, skills) {
		t.Fatal("detail crawl did not index all skill bodies")
	}
}
