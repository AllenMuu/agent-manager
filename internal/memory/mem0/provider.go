// Package mem0 implements the explicitly selected, pinned Mem0 OSS REST boundary.
package mem0

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/AllenMuu/skill-manager/internal/memory"
)

const ContractVersion = "mem0-oss-python-2.2.1-94c3fe9f238f3dbf29c9ce98643bd71eb13077cd"
const maxBody = 1024 * 1024

type Config struct {
	SecretReference *memory.ConfigReference
	Endpoint        string
	Contract        string
	AllowNetwork    bool
	Timeout         time.Duration
}
type SecretResolver interface {
	Resolve(context.Context, memory.ConfigReference) (string, error)
}
type Provider struct {
	config   Config
	client   *http.Client
	resolver SecretResolver
}

func New(c Config, resolver SecretResolver) (*Provider, error) {
	if !c.AllowNetwork || c.Contract != ContractVersion {
		return nil, memory.ErrUnsupported
	}
	u, err := url.Parse(c.Endpoint)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return nil, memory.ErrInvalidInput
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || ip == nil || !ip.IsLoopback() {
			return nil, memory.ErrInvalidInput
		}
	}
	if c.Timeout == 0 {
		c.Timeout = 10 * time.Second
	}
	if c.Timeout < time.Millisecond || c.Timeout > time.Minute {
		return nil, memory.ErrInvalidInput
	}
	if c.SecretReference != nil {
		ref := *c.SecretReference
		// Reuse the canonical opaque-reference syntax without rendering its name.
		check := memory.ProviderConfig{Version: "v1", ID: "mem0", Provider: "mem0", Configuration: ref, Scopes: []memory.Scope{memory.ScopeUser}}
		if check.Validate() != nil {
			return nil, memory.ErrInvalidInput
		}
		c.SecretReference = &ref
	}
	c.Endpoint = strings.TrimSuffix(c.Endpoint, "/")
	return &Provider{config: c, resolver: resolver, client: &http.Client{Timeout: c.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}

// Metadata stores the envelope without a second copy of content. A JSON string
// preserves uint64 counters through Python and generic JSON metadata decoders.
func metadata(r memory.Record) map[string]string {
	r.Content = ""
	b, _ := json.Marshal(r)
	return map[string]string{"am_record_id": string(r.ID), "am_owner_partition": partition(r.Owner), "am_record": string(b)}
}
func partition(o memory.Owner) string {
	b, _ := json.Marshal(o)
	return "am:" + base64.RawURLEncoding.EncodeToString(b)
}

type remoteRecord struct {
	ID       string            `json:"id"`
	Content  string            `json:"memory"`
	User     string            `json:"user_id"`
	Metadata map[string]string `json:"metadata"`
	Score    *float64          `json:"score"`
}

func (p *Provider) request(ctx context.Context, method, path string, input, output any) error {
	if ctx == nil {
		return memory.ErrInvalidInput
	}
	if ctx.Err() != nil {
		return memory.ErrCanceled
	}
	ctx, cancel := context.WithTimeout(ctx, p.config.Timeout)
	defer cancel()
	mutation := method == "POST" && path == "/memories" || method == "PUT" || method == "DELETE"
	failed := func(category error) error {
		if mutation {
			return memory.ErrOutcomeUnknown
		}
		return category
	}
	b, err := json.Marshal(input)
	if err != nil || len(b) > maxBody {
		return memory.ErrInvalidInput
	}
	req, err := http.NewRequestWithContext(ctx, method, p.config.Endpoint+path, bytes.NewReader(b))
	if err != nil {
		return memory.ErrInvalidInput
	}
	req.Header.Set("Content-Type", "application/json")
	if p.config.SecretReference != nil {
		if p.resolver == nil {
			return memory.ErrAuthentication
		}
		key, resolveErr := p.resolver.Resolve(ctx, *p.config.SecretReference)
		if ctx.Err() != nil {
			return memory.ErrCanceled
		}
		if resolveErr != nil || key == "" || strings.ContainsAny(key, "\r\n\x00") {
			return memory.ErrAuthentication
		}
		req.Header.Set("X-API-Key", key)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		if mutation {
			return memory.ErrOutcomeUnknown
		}
		if ctx.Err() != nil {
			return memory.ErrCanceled
		}
		return memory.ErrUnavailable
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 401, 403:
		return memory.ErrAuthentication
	case 409:
		return memory.ErrConflict
	case 404:
		return memory.ErrNotFound
	case 400, 422:
		return memory.ErrInvalidInput
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return failed(memory.ErrUnavailable)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil && ctx.Err() != nil {
		return failed(memory.ErrCanceled)
	}
	if err != nil || len(data) > maxBody {
		return failed(memory.ErrUnavailable)
	}
	if output == nil {
		return nil
	}
	if err = memory.ValidateCanonicalJSON(data); err != nil {
		return failed(err)
	}
	if json.Unmarshal(data, output) != nil {
		return failed(memory.ErrInvalidInput)
	}
	return nil
}
func canonical(rr remoteRecord, owner memory.Owner) (memory.Record, error) {
	var r memory.Record
	if !validBackendID(rr.ID) || memory.ValidateCanonicalJSON([]byte(rr.Metadata["am_record"])) != nil || json.Unmarshal([]byte(rr.Metadata["am_record"]), &r) != nil || r.Content != "" {
		return memory.Record{}, memory.ErrInvalidInput
	}
	r.Content = rr.Content
	if rr.User != partition(owner) || rr.Metadata["am_owner_partition"] != partition(owner) || r.Owner != owner {
		return memory.Record{}, memory.ErrOwnershipDenied
	}
	if rr.Metadata["am_record_id"] != string(r.ID) {
		return memory.Record{}, memory.ErrInvalidInput
	}
	if err := memory.ValidateRecord(r, owner); err != nil {
		return memory.Record{}, err
	}
	return r, nil
}
func (p *Provider) Remember(ctx context.Context, input memory.NewRecord) (memory.Record, error) {
	if err := memory.ValidateNewRecord(input); err != nil {
		return memory.Record{}, memory.ErrInvalidInput
	}
	id := make([]byte, 16)
	if _, err := rand.Read(id); err != nil {
		return memory.Record{}, memory.ErrUnavailable
	}
	layer := input.Layer
	if layer == "" {
		layer = memory.LayerRaw
	}
	r := memory.Record{ID: memory.RecordID("memory-" + hex.EncodeToString(id)), Version: 1, Owner: input.Owner, Type: input.Type, Content: input.Content, Source: input.Source, Evidence: input.Evidence, Layer: layer, State: memory.RecordActive}
	req := map[string]any{"messages": []any{map[string]string{"role": "user", "content": input.Content}}, "user_id": partition(input.Owner), "metadata": metadata(r), "infer": false}
	var out struct {
		Results []struct {
			ID    string `json:"id"`
			Event string `json:"event"`
		} `json:"results"`
	}
	if err := p.request(ctx, "POST", "/memories", req, &out); err != nil {
		return memory.Record{}, err
	}
	if len(out.Results) != 1 || !validBackendID(out.Results[0].ID) || out.Results[0].Event != "ADD" {
		return memory.Record{}, memory.ErrOutcomeUnknown
	}
	var remote remoteRecord
	if err := p.request(ctx, "GET", "/memories/"+url.PathEscape(out.Results[0].ID), nil, &remote); err != nil {
		return memory.Record{}, memory.ErrOutcomeUnknown
	}
	got, err := canonical(remote, input.Owner)
	if err != nil || !reflect.DeepEqual(got, r) {
		return memory.Record{}, memory.ErrOutcomeUnknown
	}
	return got, nil
}
func (p *Provider) locate(ctx context.Context, owner memory.Owner, id memory.RecordID) (remoteRecord, error) {
	if memory.ValidateOwner(owner) != nil || memory.ValidateRecordID(id) != nil {
		return remoteRecord{}, memory.ErrInvalidInput
	}
	var out searchResponse
	req := map[string]any{"query": string(id), "filters": map[string]string{"user_id": partition(owner), "am_record_id": string(id)}, "top_k": 2, "threshold": 0}
	if err := p.request(ctx, "POST", "/search", req, &out); err != nil {
		return remoteRecord{}, err
	}
	if len(out.Results) == 0 {
		return remoteRecord{}, memory.ErrNotFound
	}
	if len(out.Results) != 1 {
		return remoteRecord{}, memory.ErrConflict
	}
	rr := out.Results[0]
	r, err := canonical(rr, owner)
	if err != nil {
		return rr, err
	}
	if r.ID != id {
		return rr, memory.ErrInvalidInput
	}
	return rr, nil
}
func (p *Provider) Get(ctx context.Context, owner memory.Owner, id memory.RecordID) (memory.Record, error) {
	rr, err := p.locate(ctx, owner, id)
	if err != nil {
		return memory.Record{}, err
	}
	var out remoteRecord
	if err = p.request(ctx, "GET", "/memories/"+url.PathEscape(rr.ID), nil, &out); err != nil {
		return memory.Record{}, err
	}
	r, err := canonical(out, owner)
	if err == nil && r.ID != id {
		err = memory.ErrInvalidInput
	}
	return r, err
}
func (*Provider) Capabilities() memory.StructuredCapabilities {
	return memory.StructuredCapabilities{Remember: true, Get: true, Recall: true, ScoredRecall: true, BasicReplace: true, BasicRemove: true}
}
func (p *Provider) Health(ctx context.Context) (memory.HealthStatus, error) {
	var out searchResponse
	err := p.request(ctx, "POST", "/search", map[string]any{"query": "Agent Manager reachability probe", "filters": map[string]string{"user_id": "am:health-probe"}, "top_k": 1, "threshold": 0}, &out)
	if err != nil {
		return memory.HealthStatus{Reason: "Mem0 probe failed"}, err
	}
	return memory.HealthStatus{Available: true}, nil
}

// Recall is a bounded semantic query, never a complete storage enumeration.
func (p *Provider) Recall(ctx context.Context, q memory.Query) ([]memory.Record, error) {
	matches, err := p.RecallScored(ctx, q)
	if err != nil {
		return nil, err
	}
	records := make([]memory.Record, 0, len(matches))
	for _, m := range matches {
		records = append(records, m.Record)
	}
	return records, nil
}
func (p *Provider) Replace(ctx context.Context, input memory.ReplaceRequest) (memory.Record, error) {
	if err := memory.ValidateNewRecord(input.Record); err != nil || input.Owner != input.Record.Owner {
		return memory.Record{}, memory.ErrInvalidInput
	}
	rr, err := p.locate(ctx, input.Owner, input.ID)
	if err != nil {
		return memory.Record{}, err
	}
	old, err := canonical(rr, input.Owner)
	if err != nil {
		return memory.Record{}, err
	}
	if old.Version == ^uint64(0) {
		return memory.Record{}, memory.ErrConflict
	}
	layer := input.Record.Layer
	if layer == "" {
		layer = memory.LayerRaw
	}
	r := memory.Record{ID: old.ID, Version: old.Version + 1, Owner: input.Owner, Type: input.Record.Type, Content: input.Record.Content, Source: input.Record.Source, Evidence: input.Record.Evidence, State: memory.RecordActive, Layer: layer}
	if err = p.request(ctx, "PUT", "/memories/"+url.PathEscape(rr.ID), map[string]any{"text": r.Content, "metadata": metadata(r)}, nil); err != nil {
		return memory.Record{}, err
	}
	var out remoteRecord
	if err = p.request(ctx, "GET", "/memories/"+url.PathEscape(rr.ID), nil, &out); err != nil {
		return memory.Record{}, memory.ErrOutcomeUnknown
	}
	got, err := canonical(out, input.Owner)
	if err != nil || !reflect.DeepEqual(got, r) {
		return memory.Record{}, memory.ErrOutcomeUnknown
	}
	return got, nil
}
func (p *Provider) Remove(ctx context.Context, owner memory.Owner, id memory.RecordID) error {
	rr, err := p.locate(ctx, owner, id)
	if err != nil {
		return err
	}
	var ack struct {
		Message string `json:"message"`
	}
	if err = p.request(ctx, "DELETE", "/memories/"+url.PathEscape(rr.ID), nil, &ack); err != nil {
		return err
	}
	if ack.Message != "Memory deleted successfully" {
		return memory.ErrOutcomeUnknown
	}
	return nil
}

func validBackendID(id string) bool {
	return id != "" && len(id) <= 256 && !strings.ContainsAny(id, "/\\?#%\x00\r\n") && utf8.ValidString(id)
}
func (p *Provider) RecallScored(ctx context.Context, q memory.Query) ([]memory.ScoredRecord, error) {
	if memory.ValidateOwner(q.Owner) != nil || !utf8.ValidString(q.Text) {
		return nil, memory.ErrInvalidInput
	}
	var out searchResponse
	if err := p.request(ctx, "POST", "/search", map[string]any{"query": q.Text, "filters": map[string]string{"user_id": partition(q.Owner)}, "top_k": 100, "threshold": 0}, &out); err != nil {
		return nil, err
	}
	records := []memory.ScoredRecord{}
	seen := map[memory.RecordID]bool{}
	for _, rr := range out.Results {
		r, err := canonical(rr, q.Owner)
		if err != nil {
			return nil, err
		}
		if seen[r.ID] {
			return nil, memory.ErrConflict
		}
		seen[r.ID] = true
		if rr.Score == nil || math.IsNaN(*rr.Score) || math.IsInf(*rr.Score, 0) || *rr.Score < 0 || *rr.Score > 1 {
			return nil, memory.ErrInvalidInput
		}
		if r.State == memory.RecordActive {
			records = append(records, memory.ScoredRecord{Record: r, Score: *rr.Score})
		}
	}
	return records, nil
}

// A missing/null results field is a contract failure, never a successful empty query.
type searchResponse struct{ Results []remoteRecord }

func (s *searchResponse) UnmarshalJSON(data []byte) error {
	var out struct {
		Results *[]remoteRecord `json:"results"`
	}
	if json.Unmarshal(data, &out) != nil || out.Results == nil {
		return memory.ErrInvalidInput
	}
	s.Results = *out.Results
	return nil
}
