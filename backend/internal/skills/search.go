package skills

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/redis/go-redis/v9"

	"github.com/clear-route/orbit/internal/platform/storage"
)

const (
	skillSearchIndex          = "idx:marketplace:skills:v1"
	searchDocPrefix           = "marketplace:search:skill:"
	searchReadyPrefix         = "marketplace:search:ready:"
	maxSearchRunes            = 256
	maxSearchWords            = 32
	nonExpiringCacheSearchTTL = 12 * time.Hour
)

// SearchIndex stores and searches skills in Redis.
type SearchIndex struct {
	redis *redis.Client
	ttl   time.Duration
}

// searchGeneration describes one complete set of indexed skills.
type searchGeneration struct {
	ID        string `json:"id"`
	Count     int    `json:"count"`
	ExpiresAt int64  `json:"expiresAt"` // Unix milliseconds.
}

// NewSearchIndex uses the existing Redis connection.
func NewSearchIndex(client *storage.RedisClient, ttl time.Duration) *SearchIndex {
	return &SearchIndex{
		redis: client.Client(),
		ttl:   ttl,
	}
}

// EnsureIndex creates the search index if it does not exist.
// Use a new index name when changing its fields.
func (s *SearchIndex) EnsureIndex(ctx context.Context) error {
	err := s.redis.FTCreate(ctx, skillSearchIndex, &redis.FTCreateOptions{
		OnHash: true,
		Prefix: []any{searchDocPrefix},
		// Keep common words searchable so a query like "the" does not match every skill.
		StopWords: []any{},
	},
		&redis.FieldSchema{FieldName: "scope", FieldType: redis.SearchFieldTypeTag},
		&redis.FieldSchema{FieldName: "generation", FieldType: redis.SearchFieldTypeTag},
		&redis.FieldSchema{FieldName: "skillSlug", FieldType: redis.SearchFieldTypeTag},
		&redis.FieldSchema{FieldName: "name", FieldType: redis.SearchFieldTypeText, Weight: 5},
		&redis.FieldSchema{FieldName: "description", FieldType: redis.SearchFieldTypeText, Weight: 2},
		&redis.FieldSchema{FieldName: "body", FieldType: redis.SearchFieldTypeText, Weight: 1},
	).Err()
	if err != nil && !strings.Contains(err.Error(), "Index already exists") {
		return fmt.Errorf("create skill search index: %w: %w", ErrSearchUnavailable, err)
	}
	return nil
}

// ReplaceMarketplace writes a new set of skills, then marks it ready for search.
// Older sets expire within the cache lifetime, or within 12 hours without one.
func (s *SearchIndex) ReplaceMarketplace(ctx context.Context, tenantID, marketplaceSlug string, skills []Skill) error {
	if err := s.EnsureIndex(ctx); err != nil {
		return err
	}

	// Use the cache's remaining TTL so rebuilding does not make old data last longer.
	started := time.Now()
	remaining, err := s.redis.PTTL(ctx, MarketplaceCacheKey(tenantID, marketplaceSlug)).Result()
	if err != nil {
		return fmt.Errorf("read marketplace search lifetime: %w: %w", ErrSearchUnavailable, err)
	}
	if remaining == -2 || remaining == 0 {
		return fmt.Errorf("marketplace cache expired before indexing: %w", ErrSearchUnavailable)
	}

	lifetime := s.ttl
	if lifetime <= 0 {
		lifetime = nonExpiringCacheSearchTTL
	}
	if remaining > 0 && remaining < lifetime {
		lifetime = remaining
	}

	generation := searchGeneration{
		ID:    newSearchID(),
		Count: len(skills),
	}
	if lifetime > 0 {
		generation.ExpiresAt = started.Add(lifetime).UnixMilli()
	}

	scope := searchScope(tenantID, marketplaceSlug)
	_, err = s.redis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		for _, skill := range skills {
			key := searchDocPrefix + scope + ":" + generation.ID + ":" + searchDigest(skill.SkillSlug)
			pipe.HSet(ctx, key, map[string]any{
				"scope":           scope,
				"generation":      generation.ID,
				"tenantId":        tenantID,
				"marketplaceSlug": marketplaceSlug,
				"skillSlug":       skill.SkillSlug,
				"name":            skill.Name,
				"description":     skill.Description,
				"body":            skillBody(skill.File.Content),
			})
			if generation.ExpiresAt > 0 {
				pipe.PExpireAt(ctx, key, time.UnixMilli(generation.ExpiresAt))
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("write skill search documents: %w: %w", ErrSearchUnavailable, err)
	}

	data, err := json.Marshal(generation)
	if err != nil {
		return fmt.Errorf("encode search generation: %w", err)
	}
	if err := s.redis.Set(ctx, searchReadyPrefix+scope, data, generation.lifetime()).Err(); err != nil {
		return fmt.Errorf("publish skill search generation: %w: %w", ErrSearchUnavailable, err)
	}
	return nil
}

func (g searchGeneration) lifetime() time.Duration {
	if g.ExpiresAt == 0 {
		return 0
	}
	return max(time.Millisecond, time.Until(time.UnixMilli(g.ExpiresAt)))
}

func (s *SearchIndex) generation(ctx context.Context, scope string) (searchGeneration, error) {
	data, err := s.redis.Get(ctx, searchReadyPrefix+scope).Bytes()
	if errors.Is(err, redis.Nil) {
		return searchGeneration{}, ErrSearchIndexMissing
	}
	if err != nil {
		return searchGeneration{}, fmt.Errorf("read search generation: %w: %w", ErrSearchUnavailable, err)
	}

	var generation searchGeneration
	if err := json.Unmarshal(data, &generation); err != nil {
		return generation, fmt.Errorf("decode search generation: %w: %w", ErrSearchUnavailable, err)
	}
	if generation.ID == "" || generation.Count < 0 {
		return generation, ErrSearchUnavailable
	}
	if generation.ExpiresAt > 0 && time.Now().UnixMilli() >= generation.ExpiresAt {
		return generation, ErrSearchIndexMissing
	}
	return generation, nil
}

// Search returns skills ordered by relevance.
// A cursor continues the saved results for the same tenant, marketplace and query.
func (s *SearchIndex) Search(ctx context.Context, tenantID, marketplaceSlug, text, cursor string) (ListSkillsResponse, error) {
	words, err := searchWords(text)
	if err != nil {
		return ListSkillsResponse{}, err
	}

	scope := searchScope(tenantID, marketplaceSlug)
	queryID := searchDigest(strings.Join(words, " "))
	if cursor != "" {
		return s.searchNext(ctx, scope, queryID, cursor)
	}
	if len(words) == 0 {
		return ListSkillsResponse{Skills: []SkillSummary{}}, nil
	}

	generation, err := s.generation(ctx, scope)
	if err != nil {
		return ListSkillsResponse{}, err
	}
	matches, err := s.searchGeneration(ctx, scope, marketplaceSlug, generation, words)
	if err != nil {
		return ListSkillsResponse{}, err
	}

	// Check that the skills did not change or expire while searching.
	current, err := s.generation(ctx, scope)
	if err != nil {
		return ListSkillsResponse{}, err
	}
	if current.ID != generation.ID {
		return ListSkillsResponse{}, ErrSearchIndexMissing
	}
	return s.searchFirst(ctx, scope, queryID, generation.ExpiresAt, matches)
}

func (s *SearchIndex) searchGeneration(ctx context.Context, scope, marketplaceSlug string, generation searchGeneration, words []string) ([]SkillSummary, error) {
	if generation.Count == 0 {
		return []SkillSummary{}, nil
	}
	for _, terms := range searchQueries(words) {
		matches, err := s.searchGenerationMatches(ctx, scope, marketplaceSlug, generation, terms)
		if err != nil || len(matches) > 0 {
			return matches, err
		}
	}
	return []SkillSummary{}, nil
}

func (s *SearchIndex) searchGenerationMatches(ctx context.Context, scope, marketplaceSlug string, generation searchGeneration, terms string) ([]SkillSummary, error) {
	// IDs come from the server. Search words contain only letters and numbers.
	query := fmt.Sprintf(
		"@scope:{%s} @generation:{%s} @name|description|body:(%s)",
		scope, generation.ID, terms,
	)
	result, err := s.redis.FTSearchWithArgs(ctx, skillSearchIndex, query, &redis.FTSearchOptions{
		Scorer:     "BM25STD",
		WithScores: true,
		Limit:      generation.Count,
		Return: []redis.FTSearchReturn{
			{FieldName: "name"},
			{FieldName: "description"},
			{FieldName: "skillSlug"},
		},
		DialectVersion: 2,
	}).Result()
	if err != nil {
		message := strings.ToLower(err.Error())
		if strings.Contains(message, "no such index") || strings.Contains(message, "unknown index") {
			return nil, ErrSearchIndexMissing
		}
		return nil, fmt.Errorf("query skill search index: %w: %w", ErrSearchUnavailable, err)
	}

	if len(result.Warnings) > 0 {
		return nil, fmt.Errorf("partial skill search results: %w", ErrSearchUnavailable)
	}
	if result.Total != len(result.Docs) {
		return nil, ErrSearchIndexMissing
	}
	for _, doc := range result.Docs {
		if doc.Score == nil {
			return nil, ErrSearchUnavailable
		}
	}

	// Use the skill slug to keep equal scores in a consistent order.
	slices.SortFunc(result.Docs, compareSearchDocuments)

	matches := make([]SkillSummary, 0, len(result.Docs))
	for _, doc := range result.Docs {
		if doc.Fields["skillSlug"] == "" {
			return nil, ErrSearchIndexMissing
		}
		matches = append(matches, SkillSummary{
			Name:        doc.Fields["name"],
			Description: doc.Fields["description"],
			SkillSlug:   doc.Fields["skillSlug"],
			SkillName:   strings.TrimPrefix(doc.Fields["skillSlug"], marketplaceSlug+"__"),
		})
	}
	return matches, nil
}

func compareSearchDocuments(a, b redis.Document) int {
	if a.Score != nil && b.Score != nil {
		if *a.Score > *b.Score {
			return -1
		}
		if *a.Score < *b.Score {
			return 1
		}
	}
	return strings.Compare(a.Fields["skillSlug"], b.Fields["skillSlug"])
}

func searchWords(text string) ([]string, error) {
	if len(text) > maxSearchRunes*utf8.UTFMax || utf8.RuneCountInString(text) > maxSearchRunes {
		return nil, ErrInvalidSearch
	}
	words := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
	if len(words) > maxSearchWords {
		return nil, ErrInvalidSearch
	}
	return words, nil
}

// searchQueries tries normal words first, then bounded approximate matches.
// Every stage requires all words; only server-generated operators are added.
func searchQueries(words []string) []string {
	exact := strings.Join(words, " ")
	queries := []string{exact}
	prefix := make([]string, len(words))
	fuzzy := make([]string, len(words))
	for i, word := range words {
		prefix[i], fuzzy[i] = word, word
		length := utf8.RuneCountInString(word)
		if length >= 3 {
			prefix[i] = word + "*"
		}
		if length >= 4 {
			fuzzy[i] = "%" + word + "%"
		}
	}
	for _, terms := range []string{strings.Join(prefix, " "), strings.Join(fuzzy, " ")} {
		if terms != exact {
			queries = append(queries, terms)
		}
	}
	return queries
}

func searchScope(tenantID, marketplaceSlug string) string {
	// Include the tenant ID length so different tenant/marketplace pairs stay distinct.
	return searchDigest(fmt.Sprintf("%d:%s%s", len(tenantID), tenantID, marketplaceSlug))
}

func searchDigest(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func newSearchID() string {
	// Random letters and digits are safe to use in Redis tag filters.
	return strings.ToLower(rand.Text())
}
