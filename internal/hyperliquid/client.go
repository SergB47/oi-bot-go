package hyperliquid

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Client represents a Hyperliquid API client
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new Hyperliquid client with default settings
func NewClient() *Client {
	return &Client{
		baseURL: "https://api.hyperliquid.xyz",
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewClientWithBaseURL creates a new Hyperliquid client with custom base URL
func NewClientWithBaseURL(baseURL string) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Request represents a generic request to the info endpoint
type Request struct {
	Type string `json:"type"`
	DEX  string `json:"dex,omitempty"`
}

// GetPerpDEXs fetches all perpetual DEXes including HIP-3 markets
func (c *Client) GetPerpDEXs() ([]PerpDEX, error) {
	req := Request{
		Type: "perpDexs",
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest(
		http.MethodPost,
		c.baseURL+"/info",
		bytes.NewBuffer(reqBody),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result []interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	// Parse DEXs (first element is null for native DEX)
	var dexes []PerpDEX
	for i, item := range result {
		if item == nil {
			// Native DEX (first element is always null)
			dexes = append(dexes, PerpDEX{
				Name:     "native",
				FullName: "Hyperliquid Native",
			})
			continue
		}

		dexJSON, err := json.Marshal(item)
		if err != nil {
			continue
		}

		var dex PerpDEX
		if err := json.Unmarshal(dexJSON, &dex); err != nil {
			continue
		}

		// Handle the case where the first DEX is at index 1
		if i == 0 && dex.Name == "" {
			dex.Name = "native"
			dex.FullName = "Hyperliquid Native"
		}

		dexes = append(dexes, dex)
	}

	return dexes, nil
}

// GetMetaAndAssetCtxsForDEX fetches metadata and asset contexts for a specific DEX
func (c *Client) GetMetaAndAssetCtxsForDEX(dexName string) (*Meta, []AssetContext, error) {
	req := Request{
		Type: "metaAndAssetCtxs",
		DEX:  dexName,
	}

	// For native DEX, don't send the dex field
	if dexName == "native" || dexName == "" {
		req.DEX = ""
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest(
		http.MethodPost,
		c.baseURL+"/info",
		bytes.NewBuffer(reqBody),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result MetaAndAssetCtxsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(result) != 2 {
		return nil, nil, fmt.Errorf("unexpected response format: expected 2 elements, got %d", len(result))
	}

	// Parse meta
	metaJSON, err := json.Marshal(result[0])
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal meta: %w", err)
	}

	var meta Meta
	if err := json.Unmarshal(metaJSON, &meta); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal meta: %w", err)
	}

	// Parse asset contexts
	ctxJSON, err := json.Marshal(result[1])
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal asset contexts: %w", err)
	}

	var contexts []AssetContext
	if err := json.Unmarshal(ctxJSON, &contexts); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal asset contexts: %w", err)
	}

	return &meta, contexts, nil
}

// GetSpotMetaAndAssetCtxs fetches spot market metadata and asset contexts
func (c *Client) GetSpotMetaAndAssetCtxs() (*SpotMeta, []SpotAssetContext, error) {
	req := Request{
		Type: "spotMetaAndAssetCtxs",
	}

	reqBody, err := json.Marshal(req)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest(
		http.MethodPost,
		c.baseURL+"/info",
		bytes.NewBuffer(reqBody),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var result SpotMetaAndAssetCtxsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(result) != 2 {
		return nil, nil, fmt.Errorf("unexpected response format: expected 2 elements, got %d", len(result))
	}

	// Parse meta
	metaJSON, err := json.Marshal(result[0])
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal spot meta: %w", err)
	}

	var meta SpotMeta
	if err := json.Unmarshal(metaJSON, &meta); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal spot meta: %w", err)
	}

	// Parse asset contexts
	ctxJSON, err := json.Marshal(result[1])
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal spot asset contexts: %w", err)
	}

	var contexts []SpotAssetContext
	if err := json.Unmarshal(ctxJSON, &contexts); err != nil {
		return nil, nil, fmt.Errorf("failed to unmarshal spot asset contexts: %w", err)
	}

	return &meta, contexts, nil
}

// GetAllMarketsData fetches data from all perpetual DEXes and spot markets
func (c *Client) GetAllMarketsData() (*AllMarketsData, error) {
	result := &AllMarketsData{
		PerpData: []OpenInterestData{},
		SpotData: []OpenInterestData{},
	}

	// 1. Get all perpetual DEXes
	dexes, err := c.GetPerpDEXs()
	if err != nil {
		return nil, fmt.Errorf("failed to get perp DEXes: %w", err)
	}

	// 2. Fetch data for each DEX
	for _, dex := range dexes {
		dexName := dex.Name
		if dexName == "" {
			dexName = "native"
		}

		meta, contexts, err := c.GetMetaAndAssetCtxsForDEX(dex.Name)
		if err != nil {
			continue // Skip failed DEXes
		}

		if len(meta.Universe) != len(contexts) {
			continue
		}

		for i, asset := range meta.Universe {
			ctx := contexts[i]
			result.PerpData = append(result.PerpData, OpenInterestData{
				Coin:         asset.Name,
				DEX:          dexName,
				MarketType:   "perp",
				OpenInterest: ctx.OpenInterest,
				MarkPrice:    ctx.MarkPx,
				Funding:      ctx.Funding,
			})
		}
	}

	// 3. Get spot markets data
	spotMeta, spotContexts, err := c.GetSpotMetaAndAssetCtxs()
	if err == nil && len(spotMeta.Universe) == len(spotContexts) {
		for i, pair := range spotMeta.Universe {
			ctx := spotContexts[i]
			// For spot, we use circulating supply as "open interest" equivalent
			result.SpotData = append(result.SpotData, OpenInterestData{
				Coin:         pair.Name,
				DEX:          "spot",
				MarketType:   "spot",
				OpenInterest: ctx.CirculatingSupply,
				MarkPrice:    ctx.MarkPx,
				Funding:      "0", // Spot doesn't have funding
			})
		}
	}

	return result, nil
}

// GetOpenInterest fetches open interest data for native perpetuals only (backward compatibility)
func (c *Client) GetOpenInterest() ([]OpenInterestData, error) {
	meta, contexts, err := c.GetMetaAndAssetCtxsForDEX("native")
	if err != nil {
		return nil, err
	}

	if len(meta.Universe) == 0 || len(contexts) == 0 {
		return nil, fmt.Errorf("no data available")
	}

	// Check that universe and contexts arrays have the same length
	if len(meta.Universe) != len(contexts) {
		return nil, fmt.Errorf("mismatch between universe size (%d) and contexts count (%d)",
			len(meta.Universe), len(contexts))
	}

	var data []OpenInterestData
	for i, asset := range meta.Universe {
		ctx := contexts[i]
		data = append(data, OpenInterestData{
			Coin:         asset.Name,
			DEX:          "native",
			MarketType:   "perp",
			OpenInterest: ctx.OpenInterest,
			MarkPrice:    ctx.MarkPx,
			Funding:      ctx.Funding,
		})
	}

	return data, nil
}
