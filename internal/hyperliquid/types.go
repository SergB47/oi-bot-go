package hyperliquid

// Asset represents a single asset in the universe
type Asset struct {
	Name          string `json:"name"`
	MaxLeverage   int    `json:"maxLeverage"`
	SzDecimals    int    `json:"szDecimals"`
	MarginTableId int    `json:"marginTableId"`
	IsDelisted    bool   `json:"isDelisted,omitempty"`
}

// Meta represents the metadata response from API
type Meta struct {
	Universe []Asset `json:"universe"`
}

// AssetContext represents the context of a perpetual asset including open interest
type AssetContext struct {
	DayNtlVlm    string   `json:"dayNtlVlm"`
	Funding      string   `json:"funding"`
	ImpactPxs    []string `json:"impactPxs"`
	MarkPx       string   `json:"markPx"`
	MidPx        string   `json:"midPx"`
	OpenInterest string   `json:"openInterest"`
	OraclePx     string   `json:"oraclePx"`
	Premium      string   `json:"premium"`
	PrevDayPx    string   `json:"prevDayPx"`
}

// MetaAndAssetCtxsResponse is the response from metaAndAssetCtxs endpoint
// It returns a tuple: [meta, asset_contexts]
type MetaAndAssetCtxsResponse []interface{}

// PerpDEX represents a perpetual DEX (including HIP-3 markets)
type PerpDEX struct {
	Name              string     `json:"name"`
	FullName          string     `json:"fullName"`
	Deployer          string     `json:"deployer"`
	OracleUpdater     *string    `json:"oracleUpdater"`
	FeeRecipient      *string    `json:"feeRecipient"`
	AssetToStreamingOiCap [][]interface{} `json:"assetToStreamingOiCap"`
	AssetToFundingMultiplier [][]interface{} `json:"assetToFundingMultiplier"`
}

// SpotToken represents a token in spot markets
type SpotToken struct {
	Name                  string  `json:"name"`
	SzDecimals            int     `json:"szDecimals"`
	WeiDecimals           int     `json:"weiDecimals"`
	Index                 int     `json:"index"`
	TokenId               string  `json:"tokenId"`
	IsCanonical           bool    `json:"isCanonical"`
	DeployerTradingFeeShare string `json:"deployerTradingFeeShare"`
}

// SpotUniverseItem represents a spot trading pair
type SpotUniverseItem struct {
	Name        string `json:"name"`
	Tokens      []int  `json:"tokens"`
	Index       int    `json:"index"`
	IsCanonical bool   `json:"isCanonical"`
}

// SpotMeta represents spot metadata
type SpotMeta struct {
	Tokens   []SpotToken        `json:"tokens"`
	Universe []SpotUniverseItem `json:"universe"`
}

// SpotAssetContext represents spot asset context
type SpotAssetContext struct {
	DayNtlVlm        string `json:"dayNtlVlm"`
	MarkPx           string `json:"markPx"`
	MidPx            string `json:"midPx"`
	PrevDayPx        string `json:"prevDayPx"`
	Coin             string `json:"coin"`
	CirculatingSupply string `json:"circulatingSupply"`
	TotalSupply      string `json:"totalSupply"`
	DayBaseVlm       string `json:"dayBaseVlm"`
}

// SpotMetaAndAssetCtxsResponse is the response from spotMetaAndAssetCtxs endpoint
type SpotMetaAndAssetCtxsResponse []interface{}

// OpenInterestData represents parsed open interest data for display
type OpenInterestData struct {
	Coin         string
	DEX          string    // DEX name (e.g., "native", "xyz", "flx")
	MarketType   string    // "perp" or "spot"
	OpenInterest string
	MarkPrice    string
	Funding      string
}

// AllMarketsData represents data from all markets
type AllMarketsData struct {
	PerpData []OpenInterestData
	SpotData []OpenInterestData
}
