// Package discovery implements the four-stage capability discovery procedure.
package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"

	"github.com/phaseshiftdata/prometheus_exporters/internal/cloudflare"
)

// DatasetState represents the availability state of a dataset.
type DatasetState string

const (
	StateAvailable        DatasetState = "available"
	StateAbsentFromSchema DatasetState = "absent_from_schema"
	StateNotEntitled      DatasetState = "not_entitled"
	StateProbeFailed      DatasetState = "probe_failed"
	StateWindowExceedsRet DatasetState = "window_exceeds_retention"
)

// DatasetScope indicates account or zone scoping.
type DatasetScope string

const (
	ScopeAccount DatasetScope = "account"
	ScopeZone    DatasetScope = "zone"
)

// CandidateDataset defines a dataset the exporter wants to discover.
type CandidateDataset struct {
	Name  string
	Scope DatasetScope
}

// KnownDatasets lists all candidate dataset node names from the spec.
var KnownDatasets = []CandidateDataset{
	{Name: "accessLoginRequestsAdaptiveGroups", Scope: ScopeAccount},
	{Name: "gatewayResolverByCategoryAdaptiveGroups", Scope: ScopeAccount},
	{Name: "gatewayResolverByCustomResolverGroups", Scope: ScopeAccount},
	{Name: "gatewayL4SessionsAdaptiveGroups", Scope: ScopeAccount},
	{Name: "gatewayL4DownstreamSessionsAdaptiveGroups", Scope: ScopeAccount},
	{Name: "gatewayL4UpstreamSessionsAdaptiveGroups", Scope: ScopeAccount},
	{Name: "browserIsolationSessionsAdaptiveGroups", Scope: ScopeAccount},
	{Name: "browserIsolationUserActionsAdaptiveGroups", Scope: ScopeAccount},
	{Name: "cloudflareTunnelsAnalyticsAdaptiveGroups", Scope: ScopeAccount},
	{Name: "dnsFirewallAnalyticsAdaptiveGroups", Scope: ScopeAccount},
	{Name: "dnsAnalyticsAdaptiveGroups", Scope: ScopeZone},
}

// DatasetCapability holds the discovered state and limits for a dataset.
type DatasetCapability struct {
	Dataset      string       `json:"dataset"`
	Scope        DatasetScope `json:"scope"`
	State        DatasetState `json:"state"`
	MaxLookback  int          `json:"max_lookback_seconds,omitempty"`
	MaxTimeSpan  int          `json:"max_time_span_seconds,omitempty"`
	MaxFields    int          `json:"max_fields,omitempty"`
	MaxRecords   int          `json:"max_records,omitempty"`
	ErrorMessage string       `json:"error_message,omitempty"`
}

// AccountInfo holds discovered account metadata.
type AccountInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ZoneInfo holds discovered zone metadata.
type ZoneInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Plan   string `json:"plan"`
	Status string `json:"status"`
}

// CapabilityMatrix is the result of discovery keyed by (scope, identifier, node).
type CapabilityMatrix struct {
	mu           sync.RWMutex
	Accounts     []AccountInfo                `json:"accounts"`
	Zones        []ZoneInfo                   `json:"zones"`
	Datasets     map[string]DatasetCapability `json:"datasets"`
	DiscoveredAt time.Time                    `json:"discovered_at"`
	TokenValid   bool                         `json:"token_valid"`
}

// NewCapabilityMatrix creates an empty capability matrix.
func NewCapabilityMatrix() *CapabilityMatrix {
	return &CapabilityMatrix{
		Datasets: make(map[string]DatasetCapability),
	}
}

// SetDataset sets the capability for a dataset.
func (m *CapabilityMatrix) SetDataset(name string, cap DatasetCapability) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Datasets[name] = cap
}

// GetDataset returns the capability for a dataset.
func (m *CapabilityMatrix) GetDataset(name string) (DatasetCapability, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	cap, ok := m.Datasets[name]
	return cap, ok
}

// AvailableDatasets returns all datasets in the available state.
func (m *CapabilityMatrix) AvailableDatasets() []DatasetCapability {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var result []DatasetCapability
	for _, cap := range m.Datasets {
		if cap.State == StateAvailable {
			result = append(result, cap)
		}
	}
	return result
}

// AllDatasets returns all datasets regardless of state.
func (m *CapabilityMatrix) AllDatasets() []DatasetCapability {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]DatasetCapability, 0, len(m.Datasets))
	for _, cap := range m.Datasets {
		result = append(result, cap)
	}
	return result
}

// ToJSON serializes the matrix as JSON.
func (m *CapabilityMatrix) ToJSON() ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return json.MarshalIndent(m, "", "  ")
}

// Discovery performs the four-stage capability discovery procedure.
type Discovery struct {
	client             *cloudflare.Client
	logger             *zap.Logger
	accountIDs         []string // empty means discover all
	zoneIDs            []string // empty means discover all
	zoneExcludeIDs     []string
	scrapeDelay        int
	timeWindow         int
}

// NewDiscovery creates a new Discovery instance.
func NewDiscovery(client *cloudflare.Client, logger *zap.Logger, opts DiscoveryOptions) *Discovery {
	return &Discovery{
		client:         client,
		logger:         logger,
		accountIDs:     opts.AccountIDs,
		zoneIDs:        opts.ZoneIDs,
		zoneExcludeIDs: opts.ZoneExcludeIDs,
		scrapeDelay:    opts.ScrapeDelaySeconds,
		timeWindow:     opts.TimeWindowSeconds,
	}
}

// DiscoveryOptions configures the discovery procedure.
type DiscoveryOptions struct {
	AccountIDs         []string
	ZoneIDs            []string
	ZoneExcludeIDs     []string
	ScrapeDelaySeconds int
	TimeWindowSeconds  int
}

// Run executes the full four-stage discovery procedure.
func (d *Discovery) Run(ctx context.Context) (*CapabilityMatrix, error) {
	matrix := NewCapabilityMatrix()

	// Stage 1: Identity and scope
	d.logger.Info("discovery stage 1: verifying token and enumerating scope")
	if err := d.stage1IdentityAndScope(ctx, matrix); err != nil {
		matrix.TokenValid = false
		return matrix, fmt.Errorf("discovery stage 1 failed: %w", err)
	}
	matrix.TokenValid = true

	// Stage 2: Schema introspection
	d.logger.Info("discovery stage 2: schema introspection")
	schemaNodes := d.stage2SchemaIntrospection(ctx, matrix)

	// Stage 3: Entitlement and boundaries
	d.logger.Info("discovery stage 3: checking entitlement and boundaries")
	entitled := d.stage3Entitlement(ctx, matrix, schemaNodes)

	// Stage 4: Empirical probe
	d.logger.Info("discovery stage 4: empirical probe")
	d.stage4EmpiricalProbe(ctx, matrix, entitled)

	matrix.DiscoveredAt = time.Now().UTC()

	// Log structured summary
	d.logSummary(matrix)

	return matrix, nil
}

// stage1IdentityAndScope verifies the token and enumerates accounts and zones.
func (d *Discovery) stage1IdentityAndScope(ctx context.Context, matrix *CapabilityMatrix) error {
	// Verify token by calling /user/tokens/verify
	verifyData, _, err := d.client.RESTGet(ctx, "/user/tokens/verify")
	if err != nil {
		return fmt.Errorf("token verification failed: %w", err)
	}

	resp, err := cloudflare.ParseRESTResponse(verifyData)
	if err != nil {
		return fmt.Errorf("token verification response invalid: %w", err)
	}

	var tokenStatus struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(resp.Result, &tokenStatus); err != nil {
		return fmt.Errorf("parsing token status: %w", err)
	}
	if tokenStatus.Status != "active" {
		return fmt.Errorf("token status is %q, expected active", tokenStatus.Status)
	}

	// Enumerate accounts
	accountData, _, err := d.client.RESTGet(ctx, "/accounts?per_page=50")
	if err != nil {
		return fmt.Errorf("enumerating accounts: %w", err)
	}

	accountResp, err := cloudflare.ParseRESTResponse(accountData)
	if err != nil {
		return fmt.Errorf("parsing accounts response: %w", err)
	}

	var accounts []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(accountResp.Result, &accounts); err != nil {
		return fmt.Errorf("parsing accounts list: %w", err)
	}

	for _, a := range accounts {
		if len(d.accountIDs) > 0 && !contains(d.accountIDs, a.ID) {
			continue
		}
		matrix.Accounts = append(matrix.Accounts, AccountInfo{
			ID:   a.ID,
			Name: a.Name,
		})
	}

	if len(matrix.Accounts) == 0 {
		return fmt.Errorf("no accessible accounts found")
	}

	// Enumerate zones
	zoneData, _, err := d.client.RESTGet(ctx, "/zones?per_page=50&status=active")
	if err != nil {
		d.logger.Warn("zone enumeration failed, continuing with account-scoped only", zap.Error(err))
		return nil
	}

	zoneResp, err := cloudflare.ParseRESTResponse(zoneData)
	if err != nil {
		d.logger.Warn("parsing zones response failed", zap.Error(err))
		return nil
	}

	var zones []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Status string `json:"status"`
		Plan   struct {
			Name string `json:"name"`
		} `json:"plan"`
	}
	if err := json.Unmarshal(zoneResp.Result, &zones); err != nil {
		d.logger.Warn("parsing zones list failed", zap.Error(err))
		return nil
	}

	for _, z := range zones {
		if len(d.zoneIDs) > 0 && !contains(d.zoneIDs, z.ID) {
			continue
		}
		if contains(d.zoneExcludeIDs, z.ID) {
			continue
		}
		matrix.Zones = append(matrix.Zones, ZoneInfo{
			ID:     z.ID,
			Name:   z.Name,
			Plan:   z.Plan.Name,
			Status: z.Status,
		})
	}

	d.logger.Info("scope enumerated",
		zap.Int("accounts", len(matrix.Accounts)),
		zap.Int("zones", len(matrix.Zones)),
	)

	return nil
}

// stage2SchemaIntrospection checks which candidate nodes exist in the GraphQL schema.
func (d *Discovery) stage2SchemaIntrospection(ctx context.Context, matrix *CapabilityMatrix) []CandidateDataset {
	// Build introspection query checking for all candidate nodes
	query := `{ __schema { queryType { fields { name } } } }`

	resp, _, err := d.client.QueryGraphQL(ctx, query, nil)
	if err != nil {
		d.logger.Warn("schema introspection failed, treating all candidates as potentially available", zap.Error(err))
		// On introspection failure, pass all candidates through to Stage 3
		for _, ds := range KnownDatasets {
			matrix.SetDataset(ds.Name, DatasetCapability{
				Dataset: ds.Name,
				Scope:   ds.Scope,
				State:   StateAvailable, // tentative, will be refined in Stage 3/4
			})
		}
		return KnownDatasets
	}

	// Parse the schema fields
	var schemaData struct {
		Schema struct {
			QueryType struct {
				Fields []struct {
					Name string `json:"name"`
				} `json:"fields"`
			} `json:"queryType"`
		} `json:"__schema"`
	}
	if err := json.Unmarshal(resp.Data, &schemaData); err != nil {
		d.logger.Warn("failed to parse introspection response", zap.Error(err))
		return KnownDatasets
	}

	fieldSet := make(map[string]bool)
	for _, f := range schemaData.Schema.QueryType.Fields {
		fieldSet[f.Name] = true
	}

	// Check viewer.accounts and viewer.zones for dataset nodes
	// The GraphQL schema nests datasets under viewer -> accounts/zones
	// We need a more targeted introspection
	accountQuery := buildIntrospectionQuery("accounts")
	zoneQuery := buildIntrospectionQuery("zones")

	accountFields := d.introspectScope(ctx, accountQuery)
	zoneFields := d.introspectScope(ctx, zoneQuery)

	allFields := make(map[string]bool)
	for k, v := range accountFields {
		allFields[k] = v
	}
	for k, v := range zoneFields {
		allFields[k] = v
	}

	var surviving []CandidateDataset
	for _, ds := range KnownDatasets {
		if allFields[ds.Name] {
			surviving = append(surviving, ds)
			matrix.SetDataset(ds.Name, DatasetCapability{
				Dataset: ds.Name,
				Scope:   ds.Scope,
				State:   StateAvailable, // tentative
			})
		} else {
			matrix.SetDataset(ds.Name, DatasetCapability{
				Dataset:      ds.Name,
				Scope:        ds.Scope,
				State:        StateAbsentFromSchema,
				ErrorMessage: "node not found in GraphQL schema",
			})
			d.logger.Info("dataset absent from schema", zap.String("dataset", ds.Name))
		}
	}

	d.logger.Info("schema introspection complete",
		zap.Int("candidates", len(KnownDatasets)),
		zap.Int("found_in_schema", len(surviving)),
	)

	return surviving
}

func buildIntrospectionQuery(_ string) string {
	return `{
		__type(name: "viewer") {
			fields {
				name
				type {
					fields {
						name
						type {
							fields {
								name
							}
						}
					}
				}
			}
		}
	}`
}

func (d *Discovery) introspectScope(ctx context.Context, query string) map[string]bool {
	fields := make(map[string]bool)

	resp, _, err := d.client.QueryGraphQL(ctx, query, nil)
	if err != nil {
		d.logger.Debug("scope introspection query failed", zap.Error(err))
		// If introspection fails, assume all datasets may exist
		for _, ds := range KnownDatasets {
			fields[ds.Name] = true
		}
		return fields
	}

	// Parse nested type structure to find dataset node names
	var typeData struct {
		Type *struct {
			Fields []struct {
				Name string `json:"name"`
				Type struct {
					Fields []struct {
						Name string `json:"name"`
						Type struct {
							Fields []struct {
								Name string `json:"name"`
							} `json:"fields"`
						} `json:"type"`
					} `json:"fields"`
				} `json:"type"`
			} `json:"fields"`
		} `json:"__type"`
	}

	if err := json.Unmarshal(resp.Data, &typeData); err != nil {
		d.logger.Debug("failed to parse scope introspection", zap.Error(err))
		for _, ds := range KnownDatasets {
			fields[ds.Name] = true
		}
		return fields
	}

	if typeData.Type != nil {
		for _, f := range typeData.Type.Fields {
			for _, sf := range f.Type.Fields {
				fields[sf.Name] = true
				for _, ssf := range sf.Type.Fields {
					fields[ssf.Name] = true
				}
			}
		}
	}

	return fields
}

// stage3Entitlement queries the settings subnode for each surviving dataset.
func (d *Discovery) stage3Entitlement(ctx context.Context, matrix *CapabilityMatrix, candidates []CandidateDataset) []CandidateDataset {
	if len(matrix.Accounts) == 0 {
		return nil
	}

	accountID := matrix.Accounts[0].ID
	requiredWindow := d.scrapeDelay + d.timeWindow

	var entitled []CandidateDataset

	for _, ds := range candidates {
		var settingsQuery string
		var variables map[string]any

		if ds.Scope == ScopeAccount {
			settingsQuery = fmt.Sprintf(`{
				viewer {
					accounts(filter: {accountTag: $accountTag}) {
						%s(limit: 1) {
							settings {
								enabled
								maxDuration
								maxNumberOfFields
								maxPageSize
								notOlderThan
							}
						}
					}
				}
			}`, ds.Name)
			variables = map[string]any{"accountTag": accountID}
		} else if ds.Scope == ScopeZone && len(matrix.Zones) > 0 {
			settingsQuery = fmt.Sprintf(`{
				viewer {
					zones(filter: {zoneTag: $zoneTag}) {
						%s(limit: 1) {
							settings {
								enabled
								maxDuration
								maxNumberOfFields
								maxPageSize
								notOlderThan
							}
						}
					}
				}
			}`, ds.Name)
			variables = map[string]any{"zoneTag": matrix.Zones[0].ID}
		} else {
			// No zones available for zone-scoped dataset
			matrix.SetDataset(ds.Name, DatasetCapability{
				Dataset:      ds.Name,
				Scope:        ds.Scope,
				State:        StateNotEntitled,
				ErrorMessage: "no zones available for zone-scoped dataset",
			})
			continue
		}

		resp, _, err := d.client.QueryGraphQL(ctx, settingsQuery, variables)
		if err != nil {
			d.logger.Info("settings query failed for dataset",
				zap.String("dataset", ds.Name),
				zap.Error(err),
			)
			matrix.SetDataset(ds.Name, DatasetCapability{
				Dataset:      ds.Name,
				Scope:        ds.Scope,
				State:        StateNotEntitled,
				ErrorMessage: err.Error(),
			})
			continue
		}

		if len(resp.Errors) > 0 {
			matrix.SetDataset(ds.Name, DatasetCapability{
				Dataset:      ds.Name,
				Scope:        ds.Scope,
				State:        StateNotEntitled,
				ErrorMessage: resp.Errors[0].Message,
			})
			continue
		}

		// Parse settings response
		settings := parseSettingsFromResponse(resp.Data, ds.Name)
		if settings == nil || !settings.Enabled {
			matrix.SetDataset(ds.Name, DatasetCapability{
				Dataset:      ds.Name,
				Scope:        ds.Scope,
				State:        StateNotEntitled,
				ErrorMessage: "dataset not enabled in settings",
			})
			continue
		}

		// Check if query window exceeds retention
		if settings.NotOlderThan > 0 && requiredWindow > settings.NotOlderThan {
			matrix.SetDataset(ds.Name, DatasetCapability{
				Dataset:      ds.Name,
				Scope:        ds.Scope,
				State:        StateWindowExceedsRet,
				ErrorMessage: fmt.Sprintf("required window %ds exceeds retention %ds", requiredWindow, settings.NotOlderThan),
			})
			continue
		}

		cap := DatasetCapability{
			Dataset:     ds.Name,
			Scope:       ds.Scope,
			State:       StateAvailable, // tentative, Stage 4 will confirm
			MaxLookback: settings.NotOlderThan,
			MaxTimeSpan: settings.MaxDuration,
			MaxFields:   settings.MaxNumberOfFields,
			MaxRecords:  settings.MaxPageSize,
		}
		matrix.SetDataset(ds.Name, cap)
		entitled = append(entitled, ds)
	}

	d.logger.Info("entitlement check complete",
		zap.Int("candidates", len(candidates)),
		zap.Int("entitled", len(entitled)),
	)

	return entitled
}

// DatasetSettings holds parsed settings for a dataset node.
type DatasetSettings struct {
	Enabled           bool
	MaxDuration       int
	MaxNumberOfFields int
	MaxPageSize       int
	NotOlderThan      int
}

func parseSettingsFromResponse(data json.RawMessage, datasetName string) *DatasetSettings {
	// The response structure is deeply nested. Try to extract settings.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}

	viewerRaw, ok := raw["viewer"]
	if !ok {
		return nil
	}

	var viewer map[string]json.RawMessage
	if err := json.Unmarshal(viewerRaw, &viewer); err != nil {
		return nil
	}

	// Try accounts, then zones
	for _, scope := range []string{"accounts", "zones"} {
		scopeRaw, ok := viewer[scope]
		if !ok {
			continue
		}

		var scopes []map[string]json.RawMessage
		if err := json.Unmarshal(scopeRaw, &scopes); err != nil {
			continue
		}

		for _, s := range scopes {
			datasetRaw, ok := s[datasetName]
			if !ok {
				continue
			}

			var datasetArr []struct {
				Settings *DatasetSettings `json:"settings"`
			}
			if err := json.Unmarshal(datasetRaw, &datasetArr); err != nil {
				// Try as object
				var dataset struct {
					Settings *DatasetSettings `json:"settings"`
				}
				if err := json.Unmarshal(datasetRaw, &dataset); err != nil {
					continue
				}
				if dataset.Settings != nil {
					dataset.Settings.Enabled = true
					return dataset.Settings
				}
				continue
			}
			if len(datasetArr) > 0 && datasetArr[0].Settings != nil {
				datasetArr[0].Settings.Enabled = true
				return datasetArr[0].Settings
			}
		}
	}

	// If we got here without errors, the dataset likely exists but has no settings node
	// Treat as enabled with defaults
	return &DatasetSettings{Enabled: true}
}

// stage4EmpiricalProbe issues a minimal live query for each entitled dataset.
func (d *Discovery) stage4EmpiricalProbe(ctx context.Context, matrix *CapabilityMatrix, candidates []CandidateDataset) {
	if len(matrix.Accounts) == 0 {
		return
	}

	accountID := matrix.Accounts[0].ID
	now := time.Now().UTC()
	end := now.Add(-time.Duration(d.scrapeDelay) * time.Second).Truncate(time.Minute)
	start := end.Add(-time.Minute)

	for _, ds := range candidates {
		var query string
		var variables map[string]any

		if ds.Scope == ScopeAccount {
			query = fmt.Sprintf(`{
				viewer {
					accounts(filter: {accountTag: $accountTag}) {
						%s(
							filter: {
								datetimeMinute_geq: $start
								datetimeMinute_lt: $end
							}
							limit: 1
						) {
							count
						}
					}
				}
			}`, ds.Name)
			variables = map[string]any{
				"accountTag": accountID,
				"start":      start.Format(time.RFC3339),
				"end":        end.Format(time.RFC3339),
			}
		} else if ds.Scope == ScopeZone && len(matrix.Zones) > 0 {
			query = fmt.Sprintf(`{
				viewer {
					zones(filter: {zoneTag: $zoneTag}) {
						%s(
							filter: {
								datetimeMinute_geq: $start
								datetimeMinute_lt: $end
							}
							limit: 1
						) {
							count
						}
					}
				}
			}`, ds.Name)
			variables = map[string]any{
				"zoneTag": matrix.Zones[0].ID,
				"start":   start.Format(time.RFC3339),
				"end":     end.Format(time.RFC3339),
			}
		} else {
			continue
		}

		_, _, err := d.client.QueryGraphQL(ctx, query, variables)
		if err != nil {
			d.logger.Info("empirical probe failed for dataset",
				zap.String("dataset", ds.Name),
				zap.Error(err),
			)
			matrix.SetDataset(ds.Name, DatasetCapability{
				Dataset:      ds.Name,
				Scope:        ds.Scope,
				State:        StateProbeFailed,
				ErrorMessage: err.Error(),
			})
		} else {
			d.logger.Info("empirical probe passed", zap.String("dataset", ds.Name))
			// Keep the existing available state with limits from Stage 3
		}
	}
}

// logSummary logs the complete discovery result as one structured entry.
func (d *Discovery) logSummary(matrix *CapabilityMatrix) {
	available := 0
	absent := 0
	notEntitled := 0
	probeFailed := 0
	windowExceeds := 0

	for _, cap := range matrix.Datasets {
		switch cap.State {
		case StateAvailable:
			available++
		case StateAbsentFromSchema:
			absent++
		case StateNotEntitled:
			notEntitled++
		case StateProbeFailed:
			probeFailed++
		case StateWindowExceedsRet:
			windowExceeds++
		}
	}

	d.logger.Info("discovery complete",
		zap.Int("accounts", len(matrix.Accounts)),
		zap.Int("zones", len(matrix.Zones)),
		zap.Int("datasets_available", available),
		zap.Int("datasets_absent_from_schema", absent),
		zap.Int("datasets_not_entitled", notEntitled),
		zap.Int("datasets_probe_failed", probeFailed),
		zap.Int("datasets_window_exceeds_retention", windowExceeds),
	)
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, item) {
			return true
		}
	}
	return false
}
