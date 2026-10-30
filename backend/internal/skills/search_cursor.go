package skills

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

const searchResultPrefix = "marketplace:search:result:v2:"

type searchCursor struct {
	Snapshot string
	Offset   int
}

func encodeSearchCursor(id string, offset int) string {
	data := fmt.Appendf(nil, "2:%s:%d", id, offset)
	return base64.RawURLEncoding.EncodeToString(data)
}

func decodeSearchCursor(value string) (searchCursor, error) {
	var cursor searchCursor
	if len(value) > 512 {
		return cursor, ErrInvalidCursor
	}
	data, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return cursor, ErrInvalidCursor
	}
	parts := strings.Split(string(data), ":")
	if len(parts) != 3 || parts[0] != "2" || len(parts[1]) < 26 || len(parts[1]) > 64 {
		return cursor, ErrInvalidCursor
	}
	for _, r := range parts[1] {
		if (r < 'a' || r > 'z') && (r < '2' || r > '7') {
			return cursor, ErrInvalidCursor
		}
	}
	cursor.Snapshot = parts[1]
	cursor.Offset, err = strconv.Atoi(parts[2])
	if err != nil || cursor.Offset < serverCap || cursor.Offset%serverCap != 0 {
		return cursor, ErrInvalidCursor
	}
	return cursor, nil
}

func searchResultKey(scope, queryID, id string) string {
	return searchResultPrefix + scope + ":" + queryID + ":" + id
}

func (s *SearchIndex) searchFirst(ctx context.Context, scope, queryID string, generationExpiresAt int64, matches []SkillSummary) (ListSkillsResponse, error) {
	if len(matches) <= serverCap {
		return ListSkillsResponse{Skills: matches}, nil
	}
	expiresAt := time.Now().Add(5 * time.Minute).UnixMilli()
	if generationExpiresAt > 0 {
		expiresAt = min(expiresAt, generationExpiresAt)
	}
	id := newSearchID()
	key := searchResultKey(scope, queryID, id)
	// Each field is one complete continuation response. HGET reads a batch
	// directly, so traversing every batch decodes each summary only once.
	batches := make(map[string]any, (len(matches)-1)/serverCap)
	for start := serverCap; start < len(matches); start += serverCap {
		end := min(start+serverCap, len(matches))
		batch := ListSkillsResponse{Skills: matches[start:end]}
		if end < len(matches) {
			batch.NextCursor = encodeSearchCursor(id, end)
		}
		data, err := json.Marshal(batch)
		if err != nil {
			return ListSkillsResponse{}, fmt.Errorf("encode search batch: %w", err)
		}
		batches[strconv.Itoa(start)] = data
	}
	_, err := s.redis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ctx, key, batches)
		pipe.PExpireAt(ctx, key, time.UnixMilli(expiresAt))
		return nil
	})
	if err != nil {
		return ListSkillsResponse{}, fmt.Errorf("snapshot search results: %w: %w", ErrSearchUnavailable, err)
	}
	return ListSkillsResponse{
		Skills: matches[:serverCap], NextCursor: encodeSearchCursor(id, serverCap),
	}, nil
}

func (s *SearchIndex) searchNext(ctx context.Context, scope, queryID, value string) (ListSkillsResponse, error) {
	cursor, err := decodeSearchCursor(value)
	if err != nil {
		return ListSkillsResponse{}, err
	}
	data, err := s.redis.HGet(ctx, searchResultKey(scope, queryID, cursor.Snapshot), strconv.Itoa(cursor.Offset)).Bytes()
	if errors.Is(err, redis.Nil) {
		return ListSkillsResponse{}, ErrInvalidCursor
	}
	if err != nil {
		return ListSkillsResponse{}, fmt.Errorf("read search results: %w: %w", ErrSearchUnavailable, err)
	}
	var result ListSkillsResponse
	if err := json.Unmarshal(data, &result); err != nil {
		return ListSkillsResponse{}, fmt.Errorf("decode search results: %w: %w", ErrSearchUnavailable, err)
	}
	return result, nil
}
