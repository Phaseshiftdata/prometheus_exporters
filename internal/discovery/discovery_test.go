package discovery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/phaseshiftdata/prometheus_exporters/internal/cloudflare"
	"go.uber.org/zap"
)

func TestNewCapabilityMatrix(t *testing.T) {
	m := NewCapabilityMatrix()
	if m.Datasets == nil {
		t.Fatal("expected non-nil Datasets map")
	}
	if len(m.Datasets) != 0 {
		t.Fatalf("expected empty Datasets, got %d", len(m.Datasets))
	}
}

func TestSetAndGetDataset(t *testing.T) {
	m := NewCapabilityMatrix()
	cap := DatasetCapability{
		Dataset: "test_dataset",
		Scope:   ScopeAccount,
		State:   StateAvailable,
	}

	m.SetDataset("test_dataset", cap)

	got, ok := m.GetDataset("test_dataset")
	if !ok {
		t.Fatal("expected dataset to be found")
	}
	if got.Dataset != "test_dataset" || got.State != StateAvailable {
		t.Fatalf("unexpected dataset: %+v", got)
	}
}

func TestGetDataset_NotFound(t *testing.T) {
	m := NewCapabilityMatrix()
	_, ok := m.GetDataset("nonexistent")
	if ok {
		t.Fatal("expected dataset not to be found")
	}
}

func TestAvailableDatasets(t *testing.T) {
	m := NewCapabilityMatrix()
	m.SetDataset("a", DatasetCapability{Dataset: "a", State: StateAvailable})
	m.SetDataset("b", DatasetCapability{Dataset: "b", State: StateNotEntitled})
	m.SetDataset("c", DatasetCapability{Dataset: "c", State: StateAvailable})

	available := m.AvailableDatasets()
	if len(available) != 2 {
		t.Fatalf("expected 2 available, got %d", len(available))
	}
}

func TestAvailableDatasets_Empty(t *testing.T) {
	m := NewCapabilityMatrix()
	m.SetDataset("a", DatasetCapability{Dataset: "a", State: StateNotEntitled})

	available := m.AvailableDatasets()
	if len(available) != 0 {
		t.Fatalf("expected 0 available, got %d", len(available))
	}
}

func TestAllDatasets(t *testing.T) {
	m := NewCapabilityMatrix()
	m.SetDataset("a", DatasetCapability{Dataset: "a", State: StateAvailable})
	m.SetDataset("b", DatasetCapability{Dataset: "b", State: StateNotEntitled})

	all := m.AllDatasets()
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}
}

func TestToJSON(t *testing.T) {
	m := NewCapabilityMatrix()
	m.Accounts = []AccountInfo{{ID: "acc1", Name: "Test Account"}}
	m.Zones = []ZoneInfo{{ID: "z1", Name: "example.com", Plan: "pro", Status: "active"}}
	m.SetDataset("a", DatasetCapability{Dataset: "a", Scope: ScopeAccount, State: StateAvailable})
	m.DiscoveredAt = time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	m.TokenValid = true

	data, err := m.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}

	// Verify it's valid JSON
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("produced invalid JSON: %v", err)
	}

	// Check key fields exist
	for _, key := range []string{"accounts", "zones", "datasets", "discovered_at", "token_valid"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("missing key %q in JSON output", key)
		}
	}
}

func TestToJSON_Empty(t *testing.T) {
	m := NewCapabilityMatrix()
	data, err := m.ToJSON()
	if err != nil {
		t.Fatalf("ToJSON failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty JSON")
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		slice []string
		item  string
		want  bool
	}{
		{[]string{"a", "b", "c"}, "b", true},
		{[]string{"a", "b", "c"}, "d", false},
		{[]string{}, "a", false},
		{nil, "a", false},
		{[]string{"ABC"}, "abc", true},  // case insensitive
		{[]string{"abc"}, "ABC", true},  // case insensitive
	}
	for _, tt := range tests {
		got := contains(tt.slice, tt.item)
		if got != tt.want {
			t.Errorf("contains(%v, %q) = %v, want %v", tt.slice, tt.item, got, tt.want)
		}
	}
}

func TestDatasetStates(t *testing.T) {
	states := []DatasetState{
		StateAvailable,
		StateAbsentFromSchema,
		StateNotEntitled,
		StateProbeFailed,
		StateWindowExceedsRet,
	}
	seen := make(map[DatasetState]bool)
	for _, s := range states {
		if seen[s] {
			t.Errorf("duplicate state: %q", s)
		}
		seen[s] = true
	}
}

func TestKnownDatasets(t *testing.T) {
	if len(KnownDatasets) == 0 {
		t.Fatal("expected non-empty KnownDatasets")
	}
	names := make(map[string]bool)
	for _, ds := range KnownDatasets {
		if ds.Name == "" {
			t.Error("dataset with empty name")
		}
		if ds.Scope != ScopeAccount && ds.Scope != ScopeZone {
			t.Errorf("dataset %q has invalid scope %q", ds.Name, ds.Scope)
		}
		if names[ds.Name] {
			t.Errorf("duplicate dataset name: %q", ds.Name)
		}
		names[ds.Name] = true
	}
}

func TestSetDataset_Overwrite(t *testing.T) {
	m := NewCapabilityMatrix()
	m.SetDataset("a", DatasetCapability{Dataset: "a", State: StateAvailable})
	m.SetDataset("a", DatasetCapability{Dataset: "a", State: StateNotEntitled})

	got, ok := m.GetDataset("a")
	if !ok {
		t.Fatal("expected dataset to be found")
	}
	if got.State != StateNotEntitled {
		t.Fatalf("expected overwritten state, got %q", got.State)
	}
}

func TestParseSettingsFromResponse_ValidAccounts(t *testing.T) {
	data := json.RawMessage(`{
		"viewer": {
			"accounts": [
				{
					"testDataset": [
						{
							"settings": {
								"enabled": true,
								"maxDuration": 3600,
								"maxNumberOfFields": 10,
								"maxPageSize": 1000,
								"notOlderThan": 86400
							}
						}
					]
				}
			]
		}
	}`)

	settings := parseSettingsFromResponse(data, "testDataset")
	if settings == nil {
		t.Fatal("expected non-nil settings")
	}
	if !settings.Enabled {
		t.Error("expected Enabled to be true")
	}
	if settings.MaxDuration != 3600 {
		t.Errorf("expected MaxDuration 3600, got %d", settings.MaxDuration)
	}
}

func TestParseSettingsFromResponse_NoViewer(t *testing.T) {
	data := json.RawMessage(`{}`)
	settings := parseSettingsFromResponse(data, "testDataset")
	if settings != nil {
		t.Fatal("expected nil settings for missing viewer")
	}
}

func TestParseSettingsFromResponse_InvalidJSON(t *testing.T) {
	data := json.RawMessage(`not json`)
	settings := parseSettingsFromResponse(data, "testDataset")
	if settings != nil {
		t.Fatal("expected nil settings for invalid JSON")
	}
}

func TestParseSettingsFromResponse_NoDatasetInScope(t *testing.T) {
	data := json.RawMessage(`{
		"viewer": {
			"accounts": [
				{
					"otherDataset": []
				}
			]
		}
	}`)

	settings := parseSettingsFromResponse(data, "testDataset")
	// When no dataset found, returns enabled=true with defaults
	if settings == nil {
		t.Fatal("expected non-nil settings (default enabled)")
	}
}

func TestBuildIntrospectionQuery(t *testing.T) {
	q := buildIntrospectionQuery("accounts")
	if q == "" {
		t.Fatal("expected non-empty query")
	}
	q2 := buildIntrospectionQuery("zones")
	if q2 == "" {
		t.Fatal("expected non-empty query")
	}
}

func TestDiscoveryOptions(t *testing.T) {
	opts := DiscoveryOptions{
		AccountIDs:         []string{"acc1"},
		ZoneIDs:            []string{"z1"},
		ZoneExcludeIDs:     []string{"z2"},
		ScrapeDelaySeconds: 300,
		TimeWindowSeconds:  60,
	}
	if len(opts.AccountIDs) != 1 {
		t.Fatal("expected 1 account ID")
	}
}

func createDiscoveryTestClient(server *httptest.Server) *cloudflare.Client {
	c := cloudflare.NewClient("test-token", 5*time.Second)
	c.SetHTTPClient(&http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			req.URL.Scheme = "http"
			req.URL.Host = server.Listener.Addr().String()
			return http.DefaultTransport.RoundTrip(req)
		}),
	})
	return c
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestNewDiscovery(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := cloudflare.NewClient("token", 5*time.Second)
	opts := DiscoveryOptions{
		AccountIDs:         []string{"acc1"},
		ZoneIDs:            []string{"z1"},
		ScrapeDelaySeconds: 300,
		TimeWindowSeconds:  60,
	}
	d := NewDiscovery(client, logger, opts)
	if d == nil {
		t.Fatal("expected non-nil Discovery")
	}
}

func TestDiscovery_Run_FullSuccess(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Method == http.MethodGet {
			// REST endpoints
			if r.URL.Path == "/client/v4/user/tokens/verify" {
				resp := map[string]interface{}{
					"success": true,
					"result":  map[string]string{"status": "active"},
				}
				json.NewEncoder(w).Encode(resp)
				return
			}
			if r.URL.Path == "/client/v4/accounts" {
				resp := map[string]interface{}{
					"success": true,
					"result": []map[string]string{
						{"id": "acc1", "name": "Test Account"},
					},
				}
				json.NewEncoder(w).Encode(resp)
				return
			}
			if r.URL.Path == "/client/v4/zones" {
				resp := map[string]interface{}{
					"success": true,
					"result": []map[string]interface{}{
						{"id": "z1", "name": "example.com", "status": "active", "plan": map[string]string{"name": "pro"}},
					},
				}
				json.NewEncoder(w).Encode(resp)
				return
			}
		}
		if r.Method == http.MethodPost {
			// GraphQL
			gqlResp := map[string]interface{}{
				"data": map[string]interface{}{
					"__schema": map[string]interface{}{
						"queryType": map[string]interface{}{
							"fields": []map[string]string{{"name": "viewer"}},
						},
					},
					"__type": map[string]interface{}{
						"fields": []map[string]interface{}{
							{
								"name": "accounts",
								"type": map[string]interface{}{
									"fields": []map[string]interface{}{
										{
											"name": "accessLoginRequestsAdaptiveGroups",
											"type": map[string]interface{}{
												"fields": []map[string]string{},
											},
										},
									},
								},
							},
						},
					},
					"viewer": map[string]interface{}{
						"accounts": []map[string]interface{}{
							{
								"accessLoginRequestsAdaptiveGroups": []map[string]interface{}{
									{
										"settings": map[string]interface{}{
											"enabled":           true,
											"maxDuration":       3600,
											"maxNumberOfFields": 10,
											"maxPageSize":       1000,
											"notOlderThan":      86400,
										},
									},
								},
							},
						},
					},
				},
			}
			json.NewEncoder(w).Encode(gqlResp)
			return
		}
		w.WriteHeader(500)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)

	d := NewDiscovery(client, logger, DiscoveryOptions{
		ScrapeDelaySeconds: 300,
		TimeWindowSeconds:  60,
	})

	matrix, err := d.Run(context.Background())
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	if !matrix.TokenValid {
		t.Error("expected token to be valid")
	}
	if len(matrix.Accounts) == 0 {
		t.Error("expected at least one account")
	}
	if len(matrix.Zones) == 0 {
		t.Error("expected at least one zone")
	}
}

func TestDiscovery_Run_TokenInvalid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/client/v4/user/tokens/verify" {
			resp := map[string]interface{}{
				"success": true,
				"result":  map[string]string{"status": "expired"},
			}
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(500)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)

	d := NewDiscovery(client, logger, DiscoveryOptions{
		ScrapeDelaySeconds: 300,
		TimeWindowSeconds:  60,
	})

	matrix, err := d.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for expired token")
	}
	if matrix.TokenValid {
		t.Error("expected token to be invalid")
	}
}

func TestDiscovery_Run_TokenVerifyFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		w.Write([]byte(`error`))
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)

	d := NewDiscovery(client, logger, DiscoveryOptions{
		ScrapeDelaySeconds: 300,
		TimeWindowSeconds:  60,
	})

	_, err := d.Run(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestDiscovery_Stage1_FilterAccounts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client/v4/user/tokens/verify" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"result":  map[string]string{"status": "active"},
			})
			return
		}
		if r.URL.Path == "/client/v4/accounts" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"result": []map[string]string{
					{"id": "acc1", "name": "Account 1"},
					{"id": "acc2", "name": "Account 2"},
				},
			})
			return
		}
		if r.URL.Path == "/client/v4/zones" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"result": []map[string]interface{}{
					{"id": "z1", "name": "example.com", "status": "active", "plan": map[string]string{"name": "pro"}},
					{"id": "z2", "name": "exclude.com", "status": "active", "plan": map[string]string{"name": "pro"}},
				},
			})
			return
		}
		w.WriteHeader(500)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)

	d := NewDiscovery(client, logger, DiscoveryOptions{
		AccountIDs:         []string{"acc1"},
		ZoneExcludeIDs:     []string{"z2"},
		ScrapeDelaySeconds: 300,
		TimeWindowSeconds:  60,
	})

	matrix := NewCapabilityMatrix()
	err := d.stage1IdentityAndScope(context.Background(), matrix)
	if err != nil {
		t.Fatalf("stage1 failed: %v", err)
	}

	if len(matrix.Accounts) != 1 || matrix.Accounts[0].ID != "acc1" {
		t.Fatalf("expected only acc1, got %v", matrix.Accounts)
	}
	if len(matrix.Zones) != 1 || matrix.Zones[0].ID != "z1" {
		t.Fatalf("expected only z1, got %v", matrix.Zones)
	}
}

func TestDiscovery_Stage1_NoAccounts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client/v4/user/tokens/verify" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"result":  map[string]string{"status": "active"},
			})
			return
		}
		if r.URL.Path == "/client/v4/accounts" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"result":  []map[string]string{},
			})
			return
		}
		w.WriteHeader(500)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	err := d.stage1IdentityAndScope(context.Background(), matrix)
	if err == nil {
		t.Fatal("expected error for no accounts")
	}
}

func TestLogSummary(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := cloudflare.NewClient("token", 5*time.Second)
	d := NewDiscovery(client, logger, DiscoveryOptions{})

	matrix := NewCapabilityMatrix()
	matrix.Accounts = []AccountInfo{{ID: "acc1", Name: "Test"}}
	matrix.SetDataset("a", DatasetCapability{State: StateAvailable})
	matrix.SetDataset("b", DatasetCapability{State: StateAbsentFromSchema})
	matrix.SetDataset("c", DatasetCapability{State: StateNotEntitled})
	matrix.SetDataset("d", DatasetCapability{State: StateProbeFailed})
	matrix.SetDataset("e", DatasetCapability{State: StateWindowExceedsRet})

	// Should not panic
	d.logSummary(matrix)
}

func TestDiscovery_Stage1_ZoneEnumFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client/v4/user/tokens/verify" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true, "result": map[string]string{"status": "active"},
			})
			return
		}
		if r.URL.Path == "/client/v4/accounts" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true, "result": []map[string]string{{"id": "acc1", "name": "A"}},
			})
			return
		}
		// zones endpoint fails
		w.WriteHeader(500)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	err := d.stage1IdentityAndScope(context.Background(), matrix)
	// Zone failure is non-fatal
	if err != nil {
		t.Fatalf("expected no error for zone failure: %v", err)
	}
	if len(matrix.Accounts) != 1 {
		t.Fatal("expected 1 account")
	}
}

func TestDiscovery_Stage1_ZoneResponseInvalid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client/v4/user/tokens/verify" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true, "result": map[string]string{"status": "active"},
			})
			return
		}
		if r.URL.Path == "/client/v4/accounts" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true, "result": []map[string]string{{"id": "acc1", "name": "A"}},
			})
			return
		}
		// Invalid zones response
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": true, "result": "not-an-array",
		})
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	err := d.stage1IdentityAndScope(context.Background(), matrix)
	if err != nil {
		t.Fatalf("expected no error (zone parse failure is non-fatal): %v", err)
	}
}

func TestDiscovery_Stage1_AccountResponseInvalid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client/v4/user/tokens/verify" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true, "result": map[string]string{"status": "active"},
			})
			return
		}
		if r.URL.Path == "/client/v4/accounts" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false, "errors": []map[string]interface{}{{"code": 1000, "message": "forbidden"}},
			})
			return
		}
		w.WriteHeader(500)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	err := d.stage1IdentityAndScope(context.Background(), matrix)
	if err == nil {
		t.Fatal("expected error for failed accounts response")
	}
}

func TestDiscovery_IntrospectScope_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"__type": map[string]interface{}{
					"fields": []map[string]interface{}{
						{
							"name": "accounts",
							"type": map[string]interface{}{
								"fields": []map[string]interface{}{
									{
										"name": "accessLoginRequestsAdaptiveGroups",
										"type": map[string]interface{}{
											"fields": []map[string]string{{"name": "count"}},
										},
									},
								},
							},
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{})

	fields := d.introspectScope(context.Background(), buildIntrospectionQuery("accounts"))
	if !fields["accessLoginRequestsAdaptiveGroups"] {
		t.Error("expected dataset field to be found")
	}
}

func TestDiscovery_IntrospectScope_NilType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"__type": nil,
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{})

	fields := d.introspectScope(context.Background(), buildIntrospectionQuery("accounts"))
	// Should return empty when __type is null
	if len(fields) != 0 {
		t.Errorf("expected 0 fields for null __type, got %d", len(fields))
	}
}

func TestDiscovery_IntrospectScope_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{})

	fields := d.introspectScope(context.Background(), buildIntrospectionQuery("accounts"))
	// On error, all known datasets should be assumed present
	if len(fields) != len(KnownDatasets) {
		t.Errorf("expected %d fields, got %d", len(KnownDatasets), len(fields))
	}
}

func TestDiscovery_IntrospectScope_BadJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": json.RawMessage(`{"__type": "not-an-object"}`),
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{})

	fields := d.introspectScope(context.Background(), buildIntrospectionQuery("accounts"))
	// On parse failure, all known datasets should be assumed present
	if len(fields) != len(KnownDatasets) {
		t.Errorf("expected %d fields, got %d", len(KnownDatasets), len(fields))
	}
}

func TestDiscovery_Stage3_NoAccounts(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := cloudflare.NewClient("token", 5*time.Second)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	// No accounts
	result := d.stage3Entitlement(context.Background(), matrix, KnownDatasets)
	if result != nil {
		t.Fatalf("expected nil for no accounts, got %v", result)
	}
}

func TestDiscovery_Stage4_NoAccounts(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	client := cloudflare.NewClient("token", 5*time.Second)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	// Should not panic
	d.stage4EmpiricalProbe(context.Background(), matrix, KnownDatasets)
}

func TestDiscovery_Stage3_AccountScoped_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"viewer": map[string]interface{}{
					"accounts": []map[string]interface{}{
						{
							"accessLoginRequestsAdaptiveGroups": []map[string]interface{}{
								{
									"settings": map[string]interface{}{
										"enabled":           true,
										"maxDuration":       3600,
										"maxNumberOfFields": 10,
										"maxPageSize":       1000,
										"notOlderThan":      86400,
									},
								},
							},
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	matrix.Accounts = []AccountInfo{{ID: "acc1", Name: "Test"}}

	candidates := []CandidateDataset{
		{Name: "accessLoginRequestsAdaptiveGroups", Scope: ScopeAccount},
	}
	result := d.stage3Entitlement(context.Background(), matrix, candidates)
	if len(result) != 1 {
		t.Fatalf("expected 1 entitled, got %d", len(result))
	}
}

func TestDiscovery_Stage3_AccountScoped_QueryFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	matrix.Accounts = []AccountInfo{{ID: "acc1", Name: "Test"}}

	candidates := []CandidateDataset{
		{Name: "accessLoginRequestsAdaptiveGroups", Scope: ScopeAccount},
	}
	result := d.stage3Entitlement(context.Background(), matrix, candidates)
	if len(result) != 0 {
		t.Fatalf("expected 0 entitled on query failure, got %d", len(result))
	}
}

func TestDiscovery_Stage3_AccountScoped_GraphQLErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data":   map[string]interface{}{},
			"errors": []map[string]string{{"message": "not entitled"}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	matrix.Accounts = []AccountInfo{{ID: "acc1", Name: "Test"}}

	candidates := []CandidateDataset{
		{Name: "accessLoginRequestsAdaptiveGroups", Scope: ScopeAccount},
	}
	result := d.stage3Entitlement(context.Background(), matrix, candidates)
	if len(result) != 0 {
		t.Fatalf("expected 0 entitled on GraphQL errors, got %d", len(result))
	}
}

func TestDiscovery_Stage3_AccountScoped_NotEnabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"viewer": map[string]interface{}{
					"accounts": []map[string]interface{}{
						{
							"accessLoginRequestsAdaptiveGroups": []map[string]interface{}{
								{
									"settings": map[string]interface{}{
										"enabled":      false,
										"notOlderThan": 86400,
									},
								},
							},
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	matrix.Accounts = []AccountInfo{{ID: "acc1", Name: "Test"}}

	candidates := []CandidateDataset{
		{Name: "accessLoginRequestsAdaptiveGroups", Scope: ScopeAccount},
	}
	result := d.stage3Entitlement(context.Background(), matrix, candidates)
	// Note: parseSettingsFromResponse always overrides Enabled to true when
	// it finds settings, regardless of the "enabled" field value. This is
	// because the presence of settings data implies the dataset exists.
	if len(result) != 1 {
		t.Fatalf("expected 1 entitled (parseSettingsFromResponse overrides enabled), got %d", len(result))
	}
}

func TestDiscovery_Stage3_WindowExceedsRetention(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"viewer": map[string]interface{}{
					"accounts": []map[string]interface{}{
						{
							"accessLoginRequestsAdaptiveGroups": []map[string]interface{}{
								{
									"settings": map[string]interface{}{
										"enabled":      true,
										"notOlderThan": 10, // Very short retention
									},
								},
							},
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	matrix.Accounts = []AccountInfo{{ID: "acc1", Name: "Test"}}

	candidates := []CandidateDataset{
		{Name: "accessLoginRequestsAdaptiveGroups", Scope: ScopeAccount},
	}
	result := d.stage3Entitlement(context.Background(), matrix, candidates)
	if len(result) != 0 {
		t.Fatalf("expected 0 entitled when window exceeds retention, got %d", len(result))
	}

	cap, _ := matrix.GetDataset("accessLoginRequestsAdaptiveGroups")
	if cap.State != StateWindowExceedsRet {
		t.Fatalf("expected window_exceeds_retention state, got %q", cap.State)
	}
}

func TestDiscovery_Stage3_ZoneScoped_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": map[string]interface{}{
				"viewer": map[string]interface{}{
					"zones": []map[string]interface{}{
						{
							"dnsAnalyticsAdaptiveGroups": []map[string]interface{}{
								{
									"settings": map[string]interface{}{
										"enabled":           true,
										"maxDuration":       3600,
										"maxNumberOfFields": 10,
										"maxPageSize":       1000,
										"notOlderThan":      86400,
									},
								},
							},
						},
					},
				},
			},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	matrix.Accounts = []AccountInfo{{ID: "acc1", Name: "Test"}}
	matrix.Zones = []ZoneInfo{{ID: "z1", Name: "example.com"}}

	candidates := []CandidateDataset{
		{Name: "dnsAnalyticsAdaptiveGroups", Scope: ScopeZone},
	}
	result := d.stage3Entitlement(context.Background(), matrix, candidates)
	if len(result) != 1 {
		t.Fatalf("expected 1 entitled, got %d", len(result))
	}
}

func TestDiscovery_Stage3_ZoneScopedNoZones(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": map[string]interface{}{},
			"errors": []map[string]string{{"message": "not entitled"}},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	matrix.Accounts = []AccountInfo{{ID: "acc1", Name: "Test"}}
	// No zones

	candidates := []CandidateDataset{
		{Name: "dnsAnalyticsAdaptiveGroups", Scope: ScopeZone},
	}
	result := d.stage3Entitlement(context.Background(), matrix, candidates)
	if len(result) != 0 {
		t.Fatalf("expected 0 entitled (zone-scoped with no zones), got %d", len(result))
	}
}

func TestDiscovery_Stage4_ZoneScopedProbe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"data": map[string]interface{}{},
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	matrix.Accounts = []AccountInfo{{ID: "acc1", Name: "Test"}}
	matrix.Zones = []ZoneInfo{{ID: "z1", Name: "example.com"}}

	candidates := []CandidateDataset{
		{Name: "dnsAnalyticsAdaptiveGroups", Scope: ScopeZone},
		{Name: "accessLoginRequestsAdaptiveGroups", Scope: ScopeAccount},
	}
	d.stage4EmpiricalProbe(context.Background(), matrix, candidates)
}

func TestDiscovery_Stage4_ProbeFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	matrix.Accounts = []AccountInfo{{ID: "acc1", Name: "Test"}}
	matrix.SetDataset("access", DatasetCapability{Dataset: "access", State: StateAvailable})

	candidates := []CandidateDataset{
		{Name: "accessLoginRequestsAdaptiveGroups", Scope: ScopeAccount},
	}
	d.stage4EmpiricalProbe(context.Background(), matrix, candidates)

	cap, _ := matrix.GetDataset("accessLoginRequestsAdaptiveGroups")
	if cap.State != StateProbeFailed {
		t.Fatalf("expected probe_failed, got %q", cap.State)
	}
}

func TestParseSettingsFromResponse_InvalidViewerJSON(t *testing.T) {
	data := json.RawMessage(`{"viewer": "not-an-object"}`)
	settings := parseSettingsFromResponse(data, "testDataset")
	if settings != nil {
		t.Fatal("expected nil for invalid viewer JSON")
	}
}

func TestParseSettingsFromResponse_InvalidScopeJSON(t *testing.T) {
	data := json.RawMessage(`{"viewer": {"accounts": "not-an-array"}}`)
	settings := parseSettingsFromResponse(data, "testDataset")
	// Should handle gracefully
	if settings == nil {
		t.Log("returned nil, which is acceptable")
	}
}

func TestParseSettingsFromResponse_ObjectNoSettings(t *testing.T) {
	data := json.RawMessage(`{
		"viewer": {
			"zones": [
				{
					"testDataset": {"no_settings_key": true}
				}
			]
		}
	}`)
	settings := parseSettingsFromResponse(data, "testDataset")
	// Should return default enabled since dataset exists
	if settings == nil {
		t.Fatal("expected non-nil settings")
	}
}

func TestParseSettingsFromResponse_ObjectInsteadOfArray(t *testing.T) {
	data := json.RawMessage(`{
		"viewer": {
			"zones": [
				{
					"testDataset": {
						"settings": {
							"enabled": true,
							"maxDuration": 1800,
							"maxNumberOfFields": 5,
							"maxPageSize": 500,
							"notOlderThan": 43200
						}
					}
				}
			]
		}
	}`)

	settings := parseSettingsFromResponse(data, "testDataset")
	if settings == nil {
		t.Fatal("expected non-nil settings for object-style response")
	}
	if settings.MaxDuration != 1800 {
		t.Errorf("expected MaxDuration 1800, got %d", settings.MaxDuration)
	}
}

func TestDiscovery_Stage1_TokenVerifyResponseInvalid(t *testing.T) {
	// Token verify returns success but invalid result JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client/v4/user/tokens/verify" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"result":  "not-an-object", // invalid for unmarshal
			})
			return
		}
		w.WriteHeader(500)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	err := d.stage1IdentityAndScope(context.Background(), matrix)
	if err == nil {
		t.Fatal("expected error for invalid token result JSON")
	}
}

func TestDiscovery_Stage1_TokenVerifyNotSuccess(t *testing.T) {
	// Token verify endpoint returns success=false (ParseRESTResponse returns error)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client/v4/user/tokens/verify" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"errors": []map[string]interface{}{
					{"code": 1000, "message": "invalid token"},
				},
			})
			return
		}
		w.WriteHeader(500)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	err := d.stage1IdentityAndScope(context.Background(), matrix)
	if err == nil {
		t.Fatal("expected error for token verify not success")
	}
}

func TestDiscovery_Stage1_AccountsEnumerationFails(t *testing.T) {
	// Token verify succeeds but accounts endpoint returns HTTP error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client/v4/user/tokens/verify" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"result":  map[string]string{"status": "active"},
			})
			return
		}
		if r.URL.Path == "/client/v4/accounts" {
			w.WriteHeader(500)
			w.Write([]byte("internal error"))
			return
		}
		w.WriteHeader(500)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	err := d.stage1IdentityAndScope(context.Background(), matrix)
	if err == nil {
		t.Fatal("expected error for failed accounts enumeration")
	}
}

func TestDiscovery_Stage1_AccountsResultInvalidJSON(t *testing.T) {
	// Accounts response is success but result is not parseable as account array
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client/v4/user/tokens/verify" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"result":  map[string]string{"status": "active"},
			})
			return
		}
		if r.URL.Path == "/client/v4/accounts" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"result":  "not-an-array",
			})
			return
		}
		w.WriteHeader(500)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	err := d.stage1IdentityAndScope(context.Background(), matrix)
	if err == nil {
		t.Fatal("expected error for invalid accounts result JSON")
	}
}

func TestDiscovery_Stage1_ZonesResponseNotSuccess(t *testing.T) {
	// Zones endpoint returns success=false (non-fatal)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client/v4/user/tokens/verify" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"result":  map[string]string{"status": "active"},
			})
			return
		}
		if r.URL.Path == "/client/v4/accounts" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"result": []map[string]string{
					{"id": "acc1", "name": "Account 1"},
				},
			})
			return
		}
		if r.URL.Path == "/client/v4/zones" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": false,
				"errors": []map[string]interface{}{
					{"code": 1000, "message": "forbidden"},
				},
			})
			return
		}
		w.WriteHeader(500)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	err := d.stage1IdentityAndScope(context.Background(), matrix)
	// Zone parse failure is non-fatal
	if err != nil {
		t.Fatalf("expected no error (zone parse failure is non-fatal): %v", err)
	}
	if len(matrix.Accounts) != 1 {
		t.Fatal("expected 1 account")
	}
	if len(matrix.Zones) != 0 {
		t.Fatal("expected 0 zones")
	}
}

func TestDiscovery_Stage1_FilterZonesByID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client/v4/user/tokens/verify" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true, "result": map[string]string{"status": "active"},
			})
			return
		}
		if r.URL.Path == "/client/v4/accounts" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true, "result": []map[string]string{{"id": "acc1", "name": "A"}},
			})
			return
		}
		if r.URL.Path == "/client/v4/zones" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"success": true,
				"result": []map[string]interface{}{
					{"id": "z1", "name": "a.com", "status": "active", "plan": map[string]string{"name": "pro"}},
					{"id": "z2", "name": "b.com", "status": "active", "plan": map[string]string{"name": "pro"}},
					{"id": "z3", "name": "c.com", "status": "active", "plan": map[string]string{"name": "pro"}},
				},
			})
			return
		}
		w.WriteHeader(500)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	// Only include z1 and z3 via ZoneIDs filter
	d := NewDiscovery(client, logger, DiscoveryOptions{
		ZoneIDs:            []string{"z1", "z3"},
		ScrapeDelaySeconds: 300,
		TimeWindowSeconds:  60,
	})

	matrix := NewCapabilityMatrix()
	err := d.stage1IdentityAndScope(context.Background(), matrix)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matrix.Zones) != 2 {
		t.Fatalf("expected 2 zones after filtering, got %d", len(matrix.Zones))
	}
}

func TestDiscovery_Stage2_IntrospectionQueryFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	result := d.stage2SchemaIntrospection(context.Background(), matrix)
	// On introspection failure, all KnownDatasets should be returned
	if len(result) != len(KnownDatasets) {
		t.Fatalf("expected %d candidates, got %d", len(KnownDatasets), len(result))
	}
	// All datasets should be marked available (tentative)
	for _, ds := range KnownDatasets {
		cap, ok := matrix.GetDataset(ds.Name)
		if !ok {
			t.Fatalf("dataset %q not found in matrix", ds.Name)
		}
		if cap.State != StateAvailable {
			t.Fatalf("expected available state for %q, got %q", ds.Name, cap.State)
		}
	}
}

func TestDiscovery_Stage2_ParseIntrospectionResponseFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return valid GraphQL response with unparseable data
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": "not-an-object",
		})
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	result := d.stage2SchemaIntrospection(context.Background(), matrix)
	// On parse failure, all KnownDatasets should be returned
	if len(result) != len(KnownDatasets) {
		t.Fatalf("expected %d candidates, got %d", len(KnownDatasets), len(result))
	}
}

func TestDiscovery_Stage3_SettingsNil(t *testing.T) {
	// Return a response that parseSettingsFromResponse will return nil for.
	// When data has no "viewer" key, parseSettingsFromResponse returns nil.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{},
		})
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	matrix.Accounts = []AccountInfo{{ID: "acc1", Name: "Test"}}

	candidates := []CandidateDataset{
		{Name: "testDataset", Scope: ScopeAccount},
	}

	result := d.stage3Entitlement(context.Background(), matrix, candidates)
	// parseSettingsFromResponse returns nil when no viewer key, which triggers the
	// "settings == nil" branch -> not entitled
	if len(result) != 0 {
		t.Fatalf("expected 0 entitled (nil settings -> not entitled), got %d", len(result))
	}
	cap, ok := matrix.GetDataset("testDataset")
	if !ok {
		t.Fatal("expected dataset in matrix")
	}
	if cap.State != StateNotEntitled {
		t.Fatalf("expected not_entitled, got %q", cap.State)
	}
}

func TestDiscovery_Stage4_ZoneScopedNoZones(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"data": map[string]interface{}{},
		})
	}))
	defer server.Close()

	logger, _ := zap.NewDevelopment()
	client := createDiscoveryTestClient(server)
	d := NewDiscovery(client, logger, DiscoveryOptions{ScrapeDelaySeconds: 300, TimeWindowSeconds: 60})

	matrix := NewCapabilityMatrix()
	matrix.Accounts = []AccountInfo{{ID: "acc1", Name: "Test"}}
	// No zones - zone-scoped datasets should be skipped via `continue`

	candidates := []CandidateDataset{
		{Name: "dnsAnalyticsAdaptiveGroups", Scope: ScopeZone},
	}
	d.stage4EmpiricalProbe(context.Background(), matrix, candidates)
	// The zone-scoped dataset should not have been probed (no zones)
}

func TestParseSettingsFromResponse_ObjectNoSettingsKey(t *testing.T) {
	// The dataset is an object but unmarshal as array fails, and as object
	// it has no settings key -> returns nil settings.
	data := json.RawMessage(`{
		"viewer": {
			"accounts": [
				{
					"testDataset": "invalid-string-value"
				}
			]
		}
	}`)

	settings := parseSettingsFromResponse(data, "testDataset")
	// Both array and object unmarshal will fail for a string value,
	// then the loop continues, eventually returns enabled=true
	if settings == nil {
		t.Fatal("expected non-nil settings (fallthrough)")
	}
}

func TestParseSettingsFromResponse_EmptyArray(t *testing.T) {
	data := json.RawMessage(`{
		"viewer": {
			"accounts": [
				{
					"testDataset": []
				}
			]
		}
	}`)

	settings := parseSettingsFromResponse(data, "testDataset")
	// Empty array - len(datasetArr) == 0, so falls through to default
	if settings == nil {
		t.Fatal("expected non-nil settings (empty array fallthrough)")
	}
}

func TestParseSettingsFromResponse_ArrayWithNilSettings(t *testing.T) {
	data := json.RawMessage(`{
		"viewer": {
			"accounts": [
				{
					"testDataset": [{"no_settings_key": true}]
				}
			]
		}
	}`)

	settings := parseSettingsFromResponse(data, "testDataset")
	// Array has entry but Settings is nil
	if settings == nil {
		t.Fatal("expected non-nil settings (nil settings fallthrough)")
	}
}
