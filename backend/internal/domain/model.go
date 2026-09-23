package domain

import (
	"encoding/json"
	"time"
)

const Supplier = "SystemElectric"

func DatasetDate() time.Time { return time.Date(2026, 9, 22, 0, 0, 0, 0, time.UTC) }

type Diagnostic struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	File     string `json:"file,omitempty"`
	Sheet    string `json:"sheet,omitempty"`
	Row      int    `json:"row,omitempty"`
	Code1C   string `json:"code1C,omitempty"`
	Month    string `json:"month,omitempty"`
	Blocking bool   `json:"blocking,omitempty"`
}

type Product struct {
	Code1C           string              `json:"code1C"`
	Article          string              `json:"article"`
	Name             string              `json:"name"`
	Category         string              `json:"category"`
	Supplier         string              `json:"supplier"`
	OrderMultiple    int                 `json:"orderMultiple"`
	MonthlySales     map[string]float64  `json:"monthlySales"`
	BlankSalesMonths []string            `json:"blankSalesMonths"`
	MonthlyStock     map[string]*float64 `json:"monthlyStock"`
	Transactions     []Transaction       `json:"transactions"`
	ShowcaseStock    float64             `json:"showcaseStock"`
	TZStock          float64             `json:"tzStock"`
	RetailStock      float64             `json:"retailStock"`
	TotalStock       float64             `json:"totalStock"`
	ReservedStock    float64             `json:"reservedStock"`
	FreeStock        float64             `json:"freeStock"`
	InTransit        float64             `json:"inTransit"`
	UnitCost         *float64            `json:"unitCost"`
	Sources          map[string]bool     `json:"sources"`
	PresentFields    map[string]bool     `json:"presentFields"`
	Warnings         []Diagnostic        `json:"warnings"`
}

type Transaction struct {
	QuantityMissing bool      `json:"quantityMissing,omitempty"`
	Date            time.Time `json:"date"`
	DocumentID      string    `json:"documentId"`
	Warehouse       string    `json:"warehouse"`
	Quantity        float64   `json:"quantity"`
}

type ImportDiagnostics struct {
	ProcessedRows               map[string]int `json:"processedRows"`
	SkippedTotalRows            map[string]int `json:"skippedTotalRows"`
	UniqueProducts              int            `json:"uniqueProducts"`
	FullyMatchedProducts        int            `json:"fullyMatchedProducts"`
	ProductsWithoutMOQ          []string       `json:"productsWithoutMOQ"`
	ProductsWithoutSales        []string       `json:"productsWithoutSales"`
	ProductsWithoutCurrentStock []string       `json:"productsWithoutCurrentStock"`
	SourceConflicts             []Diagnostic   `json:"sourceConflicts"`
	Warnings                    []Diagnostic   `json:"warnings"`
	Errors                      []Diagnostic   `json:"errors"`
}

type Dataset struct {
	Supplier    string              `json:"supplier"`
	SourceFiles map[string]string   `json:"sourceFiles"`
	ID          string              `json:"datasetId"`
	AsOf        time.Time           `json:"asOf"`
	Products    map[string]*Product `json:"products"`
	Seasonality map[int]float64     `json:"seasonality"`
	Diagnostics ImportDiagnostics   `json:"diagnostics"`
}

type RunConfig struct {
	Settings           *RecommendationSettings `json:"settings,omitempty"`
	DatasetID          string                  `json:"datasetId"`
	Supplier           string                  `json:"supplier"`
	LeadTimeDays       int                     `json:"leadTimeDays"`
	SafetyDays         int                     `json:"safetyDays"`
	IncludeShowcase    bool                    `json:"includeShowcase"`
	IncludeTZStock     bool                    `json:"includeTZStock"`
	IncludeRetailStock bool                    `json:"includeRetailStock"`
}

type Anomaly struct {
	DocumentID         string  `json:"documentId"`
	Month              string  `json:"month"`
	Quantity           float64 `json:"quantity"`
	MedianTransaction  float64 `json:"medianTransaction"`
	MonthShare         float64 `json:"monthShare"`
	SimilarLargeEvents int     `json:"similarLargeEvents"`
	Deterministic      string  `json:"deterministic"`
	SourceConsistent   bool    `json:"sourceConsistent"`
	Excluded           bool    `json:"excluded"`
}

type Breakdown struct {
	PeriodStart          string   `json:"periodStart"`
	PeriodEnd            string   `json:"periodEnd"`
	BaseMonths           []string `json:"baseMonths"`
	BaseMonthlyDemand    float64  `json:"baseMonthlyDemand"`
	SeasonFactor         float64  `json:"seasonFactor"`
	SeasonalitySource    string   `json:"seasonalitySource"`
	GrowthFactor         float64  `json:"growthFactor"`
	TargetMonths         float64  `json:"targetMonths"`
	StockoutCompensation float64  `json:"stockoutCompensation"`
	StockoutPeriods      []string `json:"stockoutPeriods"`
	ForecastDemand       float64  `json:"forecastDemand"`
	SafetyStock          float64  `json:"safetyStock"`
	SafetySeasonFactor   float64  `json:"safetySeasonFactor"`
	FreeStock            float64  `json:"freeStock"`
	InTransit            float64  `json:"inTransit"`
	AdditionalStock      float64  `json:"additionalStock"`
	Available            float64  `json:"available"`
	RawNeed              float64  `json:"rawNeed"`
	OrderMultiple        int      `json:"orderMultiple"`
}

type Item struct {
	Code1C               string          `json:"code1C"`
	Article              string          `json:"article"`
	Name                 string          `json:"name"`
	Supplier             string          `json:"supplier"`
	Decision             string          `json:"decision"`
	Urgency              string          `json:"urgency"`
	RecommendedQuantity  float64         `json:"recommendedQuantity"`
	ApprovedQuantity     *float64        `json:"approvedQuantity"`
	FinalQuantity        float64         `json:"finalQuantity"`
	EstimatedCost        *float64        `json:"estimatedCost"`
	UnitCost             *float64        `json:"unitCost"`
	Breakdown            *Breakdown      `json:"breakdown,omitempty"`
	Calculation          json.RawMessage `json:"calculation,omitempty"`
	Explanation          json.RawMessage `json:"explanation,omitempty"`
	AnomalyAnalysis      json.RawMessage `json:"anomalyAnalysis,omitempty"`
	RequiresManualReview bool            `json:"requiresManualReview"`
	Confidence           *float64        `json:"confidence,omitempty"`
	Anomalies            []Anomaly       `json:"anomalies"`
	Warnings             []Diagnostic    `json:"warnings"`
	AnomalyDecision      string          `json:"anomalyDecision,omitempty"`
	ManagerComment       string          `json:"managerComment"`
}

type Summary struct {
	Buy           int     `json:"buy"`
	NoBuy         int     `json:"noBuy"`
	Review        int     `json:"review"`
	TotalUnits    float64 `json:"totalUnits"`
	EstimatedCost float64 `json:"estimatedCost"`
	UnpricedItems int     `json:"unpricedItems"`
	ApprovedItems int     `json:"approvedItems"`
}

type CalculationRun struct {
	AIResponse json.RawMessage `json:"aiResponse,omitempty"`
	ID         string          `json:"runId"`
	DatasetID  string          `json:"datasetId"`
	CreatedAt  time.Time       `json:"createdAt"`
	Config     RunConfig       `json:"config"`
	Summary    Summary         `json:"summary"`
	Items      []Item          `json:"items"`
}

// RecommendationSettings is the v1 contract of the separate calculation service.
type RecommendationSettings struct {
	HistoryMonths         int    `json:"historyMonths"`
	ForecastHorizonMonths int    `json:"forecastHorizonMonths"`
	LeadTimeDays          int    `json:"leadTimeDays"`
	SafetyStockDays       int    `json:"safetyStockDays"`
	ExcludePartialMonth   bool   `json:"excludePartialMonth"`
	AvailableStockPolicy  string `json:"availableStockPolicy"`
	AnomalyReviewEnabled  bool   `json:"anomalyReviewEnabled"`
	ExplanationMode       string `json:"explanationMode"`
	MaxAIExplanations     int    `json:"maxAIExplanations"`
}

type ManagerPatch struct {
	ApprovedQuantity *float64 `json:"approvedQuantity"`
	AnomalyDecision  *string  `json:"anomalyDecision"`
	Comment          *string  `json:"comment"`
}
