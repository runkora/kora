package skills

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type searchRedisHook struct {
	process  func(redis.Cmder)
	pipeline func([]redis.Cmder)
}

func (h searchRedisHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h searchRedisHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	if h.process == nil {
		return next
	}
	return func(_ context.Context, cmd redis.Cmder) error {
		h.process(cmd)
		return nil
	}
}

func (h searchRedisHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	if h.pipeline == nil {
		return next
	}
	return func(_ context.Context, cmds []redis.Cmder) error {
		h.pipeline(cmds)
		return nil
	}
}

func TestSearchRejectsPartialResults(t *testing.T) {
	for _, count := range []int{0, 1} {
		t.Run(fmt.Sprintf("%d matches", count), func(t *testing.T) {
			client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
			t.Cleanup(func() { _ = client.Close() })
			calls := 0
			client.AddHook(searchRedisHook{process: func(cmd redis.Cmder) {
				calls++
				score := 1.0
				result := redis.FTSearchResult{Total: count, Warnings: []string{"Timeout limit was reached"}}
				if count > 0 {
					result.Docs = []redis.Document{{ID: "one", Score: &score,
						Fields: map[string]string{"name": "Needle", "skillSlug": "repo__one"}}}
				}
				cmd.(*redis.FTSearchCmd).SetVal(result)
			}})
			index := &SearchIndex{redis: client}
			matches, err := index.searchGeneration(t.Context(), "scope", "repo",
				searchGeneration{ID: "generation", Count: 1}, []string{"needle"})
			if !errors.Is(err, ErrSearchUnavailable) || len(matches) != 0 || calls != 1 {
				t.Fatalf("partial results must fail without fallback: matches=%d, calls=%d, error=%v", len(matches), calls, err)
			}
		})
	}
}

func TestSearchSnapshotRetainsAbsoluteExpiry(t *testing.T) {
	for _, lifetime := range []time.Duration{0, time.Minute, time.Hour} {
		t.Run(lifetime.String(), func(t *testing.T) {
			client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
			t.Cleanup(func() { _ = client.Close() })
			generation := searchGeneration{ID: "generation", Count: serverCap + 1}
			if lifetime > 0 {
				generation.ExpiresAt = time.Now().Add(lifetime).UnixMilli()
			}
			data, err := json.Marshal(generation)
			if err != nil {
				t.Fatal(err)
			}
			var expiry time.Time
			client.AddHook(searchRedisHook{
				process: func(cmd redis.Cmder) {
					switch command := cmd.(type) {
					case *redis.StringCmd:
						command.SetVal(string(data))
					case *redis.FTSearchCmd:
						score := 1.0
						result := redis.FTSearchResult{Total: generation.Count}
						for i := 0; i < generation.Count; i++ {
							slug := fmt.Sprintf("repo__%03d", i)
							result.Docs = append(result.Docs, redis.Document{ID: slug, Score: &score,
								Fields: map[string]string{"name": "Needle", "skillSlug": slug}})
						}
						command.SetVal(result)
					default:
						t.Fatalf("unexpected command %s", cmd.Name())
					}
				},
				pipeline: func(cmds []redis.Cmder) {
					// Model delayed execution without sleeping: a relative TTL
					// starts at this later time, while an absolute deadline stays fixed.
					executedAt := time.Now().Add(2 * time.Hour)
					for _, cmd := range cmds {
						if cmd.Name() != "pexpire" && cmd.Name() != "pexpireat" {
							continue
						}
						milliseconds, err := strconv.ParseInt(fmt.Sprint(cmd.Args()[2]), 10, 64)
						if err != nil {
							t.Fatal(err)
						}
						if cmd.Name() == "pexpire" {
							expiry = executedAt.Add(time.Duration(milliseconds) * time.Millisecond)
						} else {
							expiry = time.UnixMilli(milliseconds)
						}
					}
				},
			})
			index := &SearchIndex{redis: client}
			result, err := index.Search(t.Context(), "tenant", "repo", "needle", "")
			if err != nil || len(result.Skills) != serverCap || result.NextCursor == "" {
				t.Fatalf("snapshot creation: matches=%d, error=%v", len(result.Skills), err)
			}
			limit := time.Now().Add(5 * time.Minute)
			if generation.ExpiresAt > 0 && time.UnixMilli(generation.ExpiresAt).Before(limit) {
				limit = time.UnixMilli(generation.ExpiresAt)
			}
			if expiry.IsZero() || expiry.After(limit) {
				t.Fatalf("snapshot expiry %v exceeds deadline %v", expiry, limit)
			}
			if lifetime == time.Minute && expiry.UnixMilli() != generation.ExpiresAt {
				t.Fatal("snapshot lost the original generation deadline")
			}
		})
	}
}

func TestSearchWords(t *testing.T) {
	for _, test := range []struct {
		text string
		want []string
	}{
		{"JWT Validation", []string{"jwt", "validation"}},
		{"needle | * @scope:{other}", []string{"needle", "scope", "other"}},
		{"security-review", []string{"security", "review"}},
		{"Проверка токени", []string{"проверка", "токени"}},
		{"***", []string{}},
	} {
		got, err := searchWords(test.text)
		if err != nil || !slices.Equal(got, test.want) {
			t.Errorf("%q: %v, %v; want %v", test.text, got, err, test.want)
		}
	}
	for _, text := range []string{strings.Repeat("a", maxSearchRunes+1), strings.Repeat("word ", maxSearchWords+1)} {
		_, err := searchWords(text)
		if !errors.Is(err, ErrInvalidSearch) {
			t.Fatalf("oversized query accepted: %v", err)
		}
	}
}

func TestSkillBody(t *testing.T) {
	for _, test := range []struct{ content, want string }{
		{"---\nname: Name\ndescription: Description\n---\n# Instructions", "# Instructions"},
		{"---\r\nname: Name\r\n---\r\nBody", "Body"},
		{"# Plain Markdown", "# Plain Markdown"},
		{"---\nnot closed", "---\nnot closed"},
		{"", ""},
	} {
		if got := skillBody(test.content); got != test.want {
			t.Errorf("body = %q, want %q", got, test.want)
		}
	}
}

func TestSearchQueries(t *testing.T) {
	for _, test := range []struct {
		words []string
		want  []string
	}{
		{[]string{"quartz"}, []string{"quartz", "quartz*", "%quartz%"}},
		{[]string{"api"}, []string{"api", "api*"}},
		{[]string{"go", "js"}, []string{"go js"}},
		{[]string{"go", "review"}, []string{"go review", "go review*", "go %review%"}},
		{[]string{"язык", "го"}, []string{"язык го", "язык* го", "%язык% го"}},
	} {
		if got := searchQueries(test.words); !slices.Equal(got, test.want) {
			t.Errorf("queries for %v = %v, want %v", test.words, got, test.want)
		}
	}
}

func TestSearchCursorValidation(t *testing.T) {
	id := newSearchID()
	value := encodeSearchCursor(id, serverCap)
	cursor, err := decodeSearchCursor(value)
	if err != nil || cursor.Snapshot != id || cursor.Offset != serverCap {
		t.Fatalf("cursor round trip: %+v, %v", cursor, err)
	}
	for _, invalid := range []string{
		"not-base64",
		encodeCursor("old-slug-cursor"),
		encodeSearchCursor(id, -1),
		encodeSearchCursor(id, serverCap+1),
		base64.RawURLEncoding.EncodeToString([]byte("1:" + id + ":200")),
		base64.RawURLEncoding.EncodeToString([]byte(`{"v":2,"snapshot":"invalid","offset":200}`)),
		strings.Repeat("a", 513),
	} {
		if _, err := decodeSearchCursor(invalid); !errors.Is(err, ErrInvalidCursor) {
			t.Errorf("accepted invalid cursor %q: %v", invalid, err)
		}
	}
}

func TestSearchScopeIsUnambiguous(t *testing.T) {
	if searchScope("a:b", "c") == searchScope("a", "b:c") ||
		searchScope("ab", "c") == searchScope("a", "bc") {
		t.Fatal("different tenant/marketplace pairs share a scope")
	}
}
