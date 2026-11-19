package pricing

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/shopspring/decimal"
	"github.com/soyaelithia-ui/ai-usage-monitor/internal/security"
)

const (
	defaultCacheTTL       = 24 * time.Hour
	defaultFetchCooldown  = 5 * time.Minute
	maxExternalBodyBytes  = 20 * 1024 * 1024
	defaultHTTPTimeoutSec = 5
)

// DefaultExternalSourceURLs lists the verifiable public pricing catalogs supported by ExternalCatalogResolver.
var DefaultExternalSourceURLs = map[string]string{
	"models.dev": "https://models.dev/api.json",
	"litellm":    "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json",
	"openrouter": "https://openrouter.ai/api/v1/models",
}

// RateResolver resolves verified rates for newly observed models that are absent from
// both agent-declared dynamic_pricing and the built-in Catalog.
type RateResolver interface {
	Resolve(provider, model string) (ModelRate, bool)
}

type cachedCatalogFile struct {
	FetchedAt string               `json:"fetchedAt"`
	Entries   map[string]ModelRate `json:"entries"`
}

// ExternalCatalogResolver resolves missing model rates optionally against verifiable
// public catalogs (models.dev, LiteLLM, OpenRouter) backed by a local disk cache.
// All rate conversions use exact decimal arithmetic with zero float64 conversions.
type ExternalCatalogResolver struct {
	CachePath            string
	SyncEnabled          bool
	Sources              []string
	URLs                 map[string]string
	TTL                  time.Duration
	HTTPClient           *http.Client
	NowFn                func() time.Time
	AllowInsecureForTest bool

	mu            sync.Mutex
	loaded        bool
	fetchedAt     time.Time
	lastSyncTryAt time.Time
	entries       map[string]ModelRate
}

// NewExternalCatalogResolver constructs a resolver with safe defaults.
func NewExternalCatalogResolver(cachePath string, syncEnabled bool, sources []string) *ExternalCatalogResolver {
	if len(sources) == 0 {
		sources = []string{"models.dev", "litellm", "openrouter"}
	}
	urls := make(map[string]string, len(DefaultExternalSourceURLs))
	for k, v := range DefaultExternalSourceURLs {
		urls[k] = v
	}
	return &ExternalCatalogResolver{
		CachePath:   cachePath,
		SyncEnabled: syncEnabled,
		Sources:     sources,
		URLs:        urls,
		TTL:         defaultCacheTTL,
		HTTPClient: &http.Client{
			Timeout: defaultHTTPTimeoutSec * time.Second,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return errors.New("too many redirects")
				}
				if req.URL.Scheme != "https" {
					return errors.New("non-https redirect rejected")
				}
				return nil
			},
		},
		NowFn:   time.Now,
		entries: make(map[string]ModelRate),
	}
}

func (r *ExternalCatalogResolver) now() time.Time {
	if r.NowFn != nil {
		return r.NowFn().UTC()
	}
	return time.Now().UTC()
}

func (r *ExternalCatalogResolver) ttl() time.Duration {
	if r.TTL > 0 {
		return r.TTL
	}
	return defaultCacheTTL
}

// Resolve attempts to look up a verified rate from the local cache or (when SyncEnabled)
// refreshes from configured public catalogs. Fails closed when unverified or deprecated.
func (r *ExternalCatalogResolver) Resolve(provider, model string) (ModelRate, bool) {
	if r == nil {
		return ModelRate{}, false
	}
	nm := NormalizeModel(model)
	if nm.Canonical == "" {
		return ModelRate{}, false
	}
	if _, deprecated := IsDeprecated(model); deprecated {
		return ModelRate{}, false
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.loaded {
		r.loadFromDiskLocked()
		r.loaded = true
	}

	if rate, ok := r.lookupLocked(nm, provider); ok {
		return rate, true
	}

	now := r.now()
	stale := r.fetchedAt.IsZero() || now.Sub(r.fetchedAt) >= r.ttl()
	canRetry := r.lastSyncTryAt.IsZero() || now.Sub(r.lastSyncTryAt) >= defaultFetchCooldown
	if r.SyncEnabled && stale && canRetry {
		r.lastSyncTryAt = now
		if r.syncFromNetworkLocked(now) {
			r.saveToDiskLocked()
			if rate, ok := r.lookupLocked(nm, provider); ok {
				return rate, true
			}
		}
	}

	return ModelRate{}, false
}

func (r *ExternalCatalogResolver) lookupLocked(nm NormalizedModel, provider string) (ModelRate, bool) {
	if len(r.entries) == 0 {
		return ModelRate{}, false
	}
	for _, cand := range nm.Candidates {
		if rate, ok := r.entries[cand]; ok {
			if ProvidersCompatible(rate.Provider, provider, nm.ProviderHint) {
				return rate, true
			}
		}
	}
	return ModelRate{}, false
}

func (r *ExternalCatalogResolver) loadFromDiskLocked() {
	if r.CachePath == "" {
		return
	}
	raw, err := os.ReadFile(r.CachePath)
	if err != nil {
		return
	}
	var cf cachedCatalogFile
	if err := json.Unmarshal(raw, &cf); err != nil || len(cf.Entries) == 0 {
		return
	}
	if cf.FetchedAt != "" {
		if t, err := ParseTimestamp(cf.FetchedAt); err == nil {
			r.fetchedAt = t
		}
	}
	for k, rate := range cf.Entries {
		if validResolvedRate(rate) {
			r.entries[k] = rate
		}
	}
}

func (r *ExternalCatalogResolver) saveToDiskLocked() {
	if r.CachePath == "" || len(r.entries) == 0 {
		return
	}
	dir := filepath.Dir(r.CachePath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	payload := cachedCatalogFile{
		FetchedAt: r.fetchedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
		Entries:   r.entries,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return
	}
	tmp := r.CachePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, r.CachePath)
	_ = os.Chmod(r.CachePath, 0o600)
}

func (r *ExternalCatalogResolver) syncFromNetworkLocked(now time.Time) bool {
	merged := make(map[string]ModelRate)
	fetchedAny := false

	// Iterate in reverse priority order so earlier sources (e.g. models.dev) win over later ones.
	for i := len(r.Sources) - 1; i >= 0; i-- {
		src := strings.ToLower(strings.TrimSpace(r.Sources[i]))
		rawURL := r.URLs[src]
		if rawURL == "" {
			continue
		}
		parsedURL, err := url.Parse(rawURL)
		if err != nil || parsedURL.Host == "" {
			continue
		}
		if parsedURL.Scheme != "https" && !r.AllowInsecureForTest {
			continue
		}

		client := r.HTTPClient
		if client == nil {
			client = &http.Client{Timeout: defaultHTTPTimeoutSec * time.Second}
		}
		req, err := http.NewRequest(http.MethodGet, rawURL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			continue
		}
		limited := io.LimitReader(resp.Body, maxExternalBodyBytes)
		var parsed map[string]ModelRate
		switch src {
		case "models.dev":
			parsed = ParseModelsDevJSON(limited)
		case "litellm":
			parsed = ParseLiteLLMJSON(limited, now)
		case "openrouter":
			parsed = ParseOpenRouterJSON(limited)
		}
		_ = resp.Body.Close()
		if len(parsed) > 0 {
			fetchedAny = true
			for k, rate := range parsed {
				merged[k] = rate
			}
		}
	}

	if fetchedAny {
		r.entries = merged
		r.fetchedAt = now
	}
	return fetchedAny
}

func validResolvedRate(rate ModelRate) bool {
	if !security.ValidRate(rate.Input) || !security.ValidRate(rate.Output) {
		return false
	}
	if rate.Cached != "" && !security.ValidRate(rate.Cached) {
		return false
	}
	if rate.CacheWrite != "" && !security.ValidRate(rate.CacheWrite) {
		return false
	}
	if rate.CacheWrite12h != "" && !security.ValidRate(rate.CacheWrite12h) {
		return false
	}
	return true
}

func parseExactDecimal(v any, scaleByMillion bool) (string, bool) {
	var raw string
	switch x := v.(type) {
	case json.Number:
		raw = strings.TrimSpace(x.String())
	case string:
		raw = strings.TrimSpace(x)
	default:
		return "", false
	}
	if raw == "" {
		return "", false
	}
	d, err := decimal.NewFromString(raw)
	if err != nil || d.IsNegative() {
		return "", false
	}
	if scaleByMillion {
		d = d.Mul(million)
	}
	s := d.String()
	if strings.ContainsAny(s, "eE") {
		s = d.StringFixed(12)
		if strings.Contains(s, ".") {
			s = strings.TrimRight(s, "0")
			s = strings.TrimRight(s, ".")
		}
	}
	if !security.ValidRate(s) {
		return "", false
	}
	return s, true
}

// ParseModelsDevJSON parses models.dev api.json using exact decimal arithmetic (USD per 1M tokens).
func ParseModelsDevJSON(r io.Reader) map[string]ModelRate {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil || root == nil {
		return nil
	}

	providersMap := root
	if wrapped, ok := root["provider"].(map[string]any); ok {
		providersMap = wrapped
	}

	out := make(map[string]ModelRate)
	for provKey, rawProv := range providersMap {
		provObj, ok := rawProv.(map[string]any)
		if !ok {
			continue
		}
		modelsObj, ok := provObj["models"].(map[string]any)
		if !ok {
			continue
		}
		normProv := NormalizeProvider(provKey)
		for modelKey, rawModel := range modelsObj {
			modelObj, ok := rawModel.(map[string]any)
			if !ok {
				continue
			}
			costObj, ok := modelObj["cost"].(map[string]any)
			if !ok {
				continue
			}
			inStr, okIn := parseExactDecimal(costObj["input"], false)
			outStr, okOut := parseExactDecimal(costObj["output"], false)
			if !okIn || !okOut {
				continue
			}
			cachedStr, _ := parseExactDecimal(costObj["cache_read"], false)
			cwStr, _ := parseExactDecimal(costObj["cache_write"], false)

			nm := NormalizeModel(modelKey)
			if nm.Canonical == "" {
				continue
			}
			prov := normProv
			if nm.ProviderHint != "" && (prov == "" || IsAggregatorProvider(prov)) {
				prov = nm.ProviderHint
			}
			rate := ModelRate{
				Provider:   prov,
				Input:      inStr,
				Cached:     cachedStr,
				CacheWrite: cwStr,
				Output:     outStr,
				Source:     "models.dev",
			}
			if !validResolvedRate(rate) {
				continue
			}
			out[strings.ToLower(strings.TrimSpace(modelKey))] = rate
			if _, exists := out[nm.Canonical]; !exists {
				out[nm.Canonical] = rate
			}
		}
	}
	return out
}

// ParseLiteLLMJSON parses LiteLLM model_prices_and_context_window.json using exact decimal arithmetic.
// LiteLLM stores per-token USD prices, scaled exactly by 1,000,000.
func ParseLiteLLMJSON(r io.Reader, now time.Time) map[string]ModelRate {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil || root == nil {
		return nil
	}

	out := make(map[string]ModelRate)
	for modelKey, rawEntry := range root {
		if modelKey == "sample_spec" {
			continue
		}
		entry, ok := rawEntry.(map[string]any)
		if !ok {
			continue
		}
		if mode, ok := entry["mode"].(string); ok && mode != "" {
			if mode != "chat" && mode != "completion" && mode != "responses" {
				continue
			}
		}
		if depRaw, ok := entry["deprecation_date"].(string); ok && depRaw != "" {
			if depTime, err := time.Parse("2006-01-02", strings.TrimSpace(depRaw)); err == nil && !now.IsZero() && !now.Before(depTime) {
				continue
			}
		}

		inStr, okIn := parseExactDecimal(entry["input_cost_per_token"], true)
		outStr, okOut := parseExactDecimal(entry["output_cost_per_token"], true)
		if !okIn || !okOut {
			continue
		}
		cachedStr, _ := parseExactDecimal(entry["cache_read_input_token_cost"], true)
		cwStr, _ := parseExactDecimal(entry["cache_creation_input_token_cost"], true)

		nm := NormalizeModel(modelKey)
		if nm.Canonical == "" {
			continue
		}
		prov := ""
		if lp, ok := entry["litellm_provider"].(string); ok {
			prov = NormalizeProvider(lp)
		}
		if nm.ProviderHint != "" && (prov == "" || IsAggregatorProvider(prov)) {
			prov = nm.ProviderHint
		}
		rate := ModelRate{
			Provider:   prov,
			Input:      inStr,
			Cached:     cachedStr,
			CacheWrite: cwStr,
			Output:     outStr,
			Source:     "litellm",
		}
		if !validResolvedRate(rate) {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(modelKey))] = rate
		if _, exists := out[nm.Canonical]; !exists {
			out[nm.Canonical] = rate
		}
	}
	return out
}

// ParseOpenRouterJSON parses OpenRouter /api/v1/models JSON using exact decimal arithmetic.
// OpenRouter stores per-token USD decimal strings, scaled exactly by 1,000,000.
func ParseOpenRouterJSON(r io.Reader) map[string]ModelRate {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	var root map[string]any
	if err := dec.Decode(&root); err != nil || root == nil {
		return nil
	}
	dataSlice, ok := root["data"].([]any)
	if !ok {
		return nil
	}

	out := make(map[string]ModelRate)
	for _, item := range dataSlice {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		id, _ := obj["id"].(string)
		pricingObj, ok := obj["pricing"].(map[string]any)
		if !ok || strings.TrimSpace(id) == "" {
			continue
		}
		inStr, okIn := parseExactDecimal(pricingObj["prompt"], true)
		outStr, okOut := parseExactDecimal(pricingObj["completion"], true)
		if !okIn || !okOut {
			continue
		}
		cachedStr, _ := parseExactDecimal(pricingObj["input_cache_read"], true)
		cwStr, _ := parseExactDecimal(pricingObj["input_cache_write"], true)

		nm := NormalizeModel(id)
		if nm.Canonical == "" {
			continue
		}
		prov := "openrouter"
		if nm.ProviderHint != "" {
			prov = nm.ProviderHint
		}
		rate := ModelRate{
			Provider:   prov,
			Input:      inStr,
			Cached:     cachedStr,
			CacheWrite: cwStr,
			Output:     outStr,
			Source:     "openrouter",
		}
		if !validResolvedRate(rate) {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(id))] = rate
		if !nm.IsFreeVariant {
			if _, exists := out[nm.Canonical]; !exists {
				out[nm.Canonical] = rate
			}
		} else {
			out[nm.Canonical] = rate
		}
	}
	return out
}
