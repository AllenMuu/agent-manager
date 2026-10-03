package memory

import (
	"context"
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SearchRequest asks for current knowledge in one exact authorized partition.
// Retired lineage is available only through explicit Get/History operations.
type SearchRequest struct {
	Owner Owner         `json:"owner"`
	Text  string        `json:"text,omitempty"`
	Type  KnowledgeType `json:"type,omitempty"`
}

// ScoredRecordRecaller is an optional provider boundary for normalized finite
// relevance scores in [0,1]. Plain Recall remains source compatible. A score
// must originate from the provider, never an invented semantic estimate.
type ScoredRecord struct {
	Record Record  `json:"record"`
	Score  float64 `json:"score"`
}
type ScoredRecordRecaller interface {
	RecallScored(context.Context, Query) ([]ScoredRecord, error)
}

func (p RetrievalPolicy) normalized() (RetrievalPolicy, error) {
	if p.MaxResults == 0 {
		p.MaxResults = 10
	}
	if p.ContentBytes == 0 {
		p.ContentBytes = 32 * 1024
	}
	if p.ContextBytes == 0 {
		p.ContextBytes = 128 * 1024
	}
	if p.MaxResults < 1 || p.MaxResults > 100 || p.ContentBytes < 1 || p.ContentBytes > 1024*1024 || p.ContextBytes < 2 || p.ContextBytes > 1024*1024 || math.IsNaN(p.MinRelevance) || math.IsInf(p.MinRelevance, 0) || p.MinRelevance < 0 || p.MinRelevance > 1 {
		return p, ErrInvalidInput
	}
	return p, nil
}
func (g *Gateway) Search(ctx context.Context, request SearchRequest) ([]Record, error) {
	if err := g.authorize(request.Owner, false); err != nil {
		return nil, err
	}
	if err := operationContext(ctx); err != nil {
		return nil, err
	}
	if !utf8.ValidString(request.Text) || (request.Type != "" && !knownType(request.Type)) {
		return nil, ErrInvalidInput
	}
	type candidate struct {
		record Record
		score  float64
	}
	candidates := []candidate{}
	scored, hasScores := g.provider.(ScoredRecordRecaller)
	var matches []ScoredRecord
	if hasScores && g.provider.Capabilities().ScoredRecall {
		if err := providerReady(ctx, g.provider, true); err != nil {
			return nil, SafeError(err)
		}
		results, err := scored.RecallScored(ctx, Query{Owner: request.Owner, Text: request.Text})
		if err != nil {
			return nil, SafeError(err)
		}
		matches = results
	} else {
		// Local fallback ranks all owned current candidates by lexical token
		// coverage. It does not claim a semantic reranker.
		records, err := Recall(ctx, g.provider, Query{Owner: request.Owner})
		if err != nil {
			return nil, SafeError(err)
		}
		for _, r := range records {
			matches = append(matches, ScoredRecord{Record: r, Score: lexicalRelevance(request.Text, r.Content)})
		}
	}
	seen := map[RecordID]ScoredRecord{}
	for _, match := range matches {
		r, score := match.Record, match.Score
		if err := validateGatewayRecord(r, request.Owner); err != nil {
			return nil, err
		}
		if previous, ok := seen[r.ID]; ok {
			if !reflect.DeepEqual(previous, match) {
				return nil, ErrInvalidInput
			}
			continue
		}
		seen[r.ID] = match
		if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 || score > 1 {
			return nil, ErrInvalidInput
		}
		if r.State != RecordActive || (request.Type != "" && r.Type != request.Type) {
			continue
		}
		if score < g.policy.MinRelevance || (strings.TrimSpace(request.Text) != "" && score == 0) {
			continue
		}
		candidates = append(candidates, candidate{record: cloneRecord(r), score: score})
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].record.ID < candidates[j].record.ID
	})
	selected := []Record{}
	contentBytes, contextBytes := 0, 2
	for _, candidate := range candidates {
		r := candidate.record
		encoded, err := json.Marshal(r)
		if err != nil {
			return nil, ErrInvalidInput
		}
		separator := 0
		if len(selected) > 0 {
			separator = 1
		}
		if len(selected) >= g.policy.MaxResults {
			break
		}
		if len(r.Content) > g.policy.ContentBytes-contentBytes || len(encoded)+separator > g.policy.ContextBytes-contextBytes {
			continue
		}
		selected = append(selected, r)
		contentBytes += len(r.Content)
		contextBytes += len(encoded) + separator
	}
	return selected, nil
}
func lexicalRelevance(query, content string) float64 {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return 1
	}
	tokens := strings.FieldsFunc(query, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	unique := map[string]bool{}
	for _, token := range tokens {
		unique[token] = true
	}
	if len(unique) == 0 {
		return 0
	}
	matched := 0
	content = strings.ToLower(content)
	for token := range unique {
		if strings.Contains(content, token) {
			matched++
		}
	}
	return float64(matched) / float64(len(unique))
}
