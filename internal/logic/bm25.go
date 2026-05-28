package logic

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var tokenSplitRE = regexp.MustCompile(`[\p{Han}]|[A-Za-z0-9_+\-\.]+`)

type bm25Stats struct {
	avgDocLen float64
	totalDocs int
	docFreq   map[string]int
}

type bm25Encoder struct {
	mu    sync.RWMutex
	stats bm25Stats
	k1    float64
	b     float64
}

var globalBM25 = &bm25Encoder{k1: 1.2, b: 0.75}

func tokenizeForSparse(text string) []string {
	matches := tokenSplitRE.FindAllString(strings.ToLower(text), -1)
	if len(matches) == 0 {
		return nil
	}
	tokens := make([]string, 0, len(matches))
	for _, token := range matches {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens
}

func (e *bm25Encoder) Rebuild(corpus []string) {
	stats := bm25Stats{
		docFreq: make(map[string]int),
	}
	totalLen := 0
	for _, doc := range corpus {
		tokens := tokenizeForSparse(doc)
		if len(tokens) == 0 {
			continue
		}
		stats.totalDocs++
		totalLen += len(tokens)

		seen := make(map[string]struct{}, len(tokens))
		for _, token := range tokens {
			if _, ok := seen[token]; ok {
				continue
			}
			seen[token] = struct{}{}
			stats.docFreq[token]++
		}
	}
	if stats.totalDocs > 0 {
		stats.avgDocLen = float64(totalLen) / float64(stats.totalDocs)
	}

	e.mu.Lock()
	e.stats = stats
	e.mu.Unlock()
}

func (e *bm25Encoder) Score(query, doc string) float64 {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if e.stats.totalDocs == 0 {
		return 0
	}

	queryTokens := tokenizeForSparse(query)
	docTokens := tokenizeForSparse(doc)
	if len(queryTokens) == 0 || len(docTokens) == 0 {
		return 0
	}

	tf := make(map[string]int, len(docTokens))
	for _, token := range docTokens {
		tf[token]++
	}
	docLen := float64(len(docTokens))
	score := 0.0
	for _, token := range queryTokens {
		freq := tf[token]
		if freq == 0 {
			continue
		}
		df := e.stats.docFreq[token]
		if df == 0 {
			continue
		}
		idf := math.Log(1 + (float64(e.stats.totalDocs-df)+0.5)/(float64(df)+0.5))
		numerator := float64(freq) * (e.k1 + 1)
		denominator := float64(freq) + e.k1*(1-e.b+e.b*(docLen/e.stats.avgDocLen))
		if denominator <= 0 {
			continue
		}
		score += idf * (numerator / denominator)
	}
	return score
}

func (e *bm25Encoder) BulkScore(query string, docs []string) []float64 {
	result := make([]float64, len(docs))
	for i, doc := range docs {
		result[i] = e.Score(query, doc)
	}
	return result
}

func rankByScore(scores map[int64]float64, topK int) []int64 {
	type pair struct {
		id    int64
		score float64
	}
	items := make([]pair, 0, len(scores))
	for id, score := range scores {
		items = append(items, pair{id: id, score: score})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].score == items[j].score {
			return items[i].id < items[j].id
		}
		return items[i].score > items[j].score
	})
	if topK > 0 && len(items) > topK {
		items = items[:topK]
	}
	result := make([]int64, 0, len(items))
	for _, item := range items {
		result = append(result, item.id)
	}
	return result
}
