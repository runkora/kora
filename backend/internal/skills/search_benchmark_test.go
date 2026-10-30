package skills

import (
	"fmt"
	"testing"
	"time"
)

// Run with ORBIT_TEST_REDIS_URL against an isolated Redis with Search/BM25STD.
// This measures a warm search and all of its continuation batches, not ingestion.
func BenchmarkRedisSearchAllResults(b *testing.B) {
	for _, count := range []int{100, 1000, 10000} {
		for _, query := range []string{"quartzprobe", "quartz", "quartzprbe"} {
			b.Run(fmt.Sprintf("%d/%s", count, query), func(b *testing.B) {
				index, tenant := redisSearchForTest(b, 5*time.Minute)
				skills := make([]Skill, count)
				for i := range skills {
					skills[i] = Skill{
						Name:        "Quartzprobe",
						Description: "Catalogue",
						SkillSlug:   fmt.Sprintf("repo__%05d", i),
						File:        SkillFile{Content: fmt.Sprintf("Instructions item%d", i)},
					}
				}
				replaceSearchForTest(b, index, tenant, "repo", skills)
				b.ReportAllocs()
				for b.Loop() {
					cursor := ""
					total := 0
					for {
						result, err := index.Search(b.Context(), tenant, "repo", query, cursor)
						if err != nil {
							b.Fatal(err)
						}
						total += len(result.Skills)
						cursor = result.NextCursor
						if cursor == "" {
							break
						}
					}
					if total != count {
						b.Fatalf("received %d Skills, want %d", total, count)
					}
				}
			})
		}
	}
}
