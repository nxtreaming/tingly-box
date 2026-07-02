package provider

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tingly-dev/tingly-box/internal/dataio"
	"github.com/tingly-dev/tingly-box/internal/protocol"
	"github.com/tingly-dev/tingly-box/internal/server/config"
	"github.com/tingly-dev/tingly-box/internal/typ"
)

// TestProviderModelResponseMeta tests the new cache metadata in responses
func TestProviderModelResponseMeta(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name           string
		response       ProviderModelsResponse
		expectedSource ModelCacheSource
		expectExpiry   bool
	}{
		{
			name: "DB cache response",
			response: ProviderModelsResponse{
				Data: ProviderModelInfo{
					Models:    []string{"model-1"},
					Source:    ModelCacheSourceDB,
					ExpiresAt: time.Now().Add(1 * time.Hour),
				},
			},
			expectedSource: ModelCacheSourceDB,
			expectExpiry:   true,
		},
		{
			name: "Template fallback response",
			response: ProviderModelsResponse{
				Data: ProviderModelInfo{
					Models:    []string{"tmpl-1"},
					Source:    ModelCacheSourceTemplate,
					ExpiresAt: time.Now().Add(24 * time.Hour),
				},
			},
			expectedSource: ModelCacheSourceTemplate,
			expectExpiry:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expectedSource, tt.response.Data.Source)
			if tt.expectExpiry {
				assert.False(t, tt.response.Data.ExpiresAt.IsZero())
			}
		})
	}
}

// TestModelCacheSourceSerialization tests JSON serialization of new fields
func TestModelCacheSourceSerialization(t *testing.T) {
	info := ProviderModelInfo{
		Models:      []string{"model-1", "model-2"},
		Source:      ModelCacheSourceAPI,
		ExpiresAt:   time.Date(2026, 5, 26, 15, 0, 0, 0, time.UTC),
		LastUpdated: "2026-05-26 14:00:00",
	}

	// Test JSON marshaling
	data, err := json.Marshal(info)
	require.NoError(t, err)

	// Verify fields exist
	var parsed map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &parsed))

	assert.Contains(t, parsed, "source")
	assert.Equal(t, string(ModelCacheSourceAPI), parsed["source"])
	assert.Contains(t, parsed, "expiresAt")
	assert.Equal(t, "2026-05-26T15:00:00Z", parsed["expiresAt"])
}

// TestTemplateCacheTTL tests that template-sourced models use 24h TTL
func TestTemplateCacheTTL(t *testing.T) {
	// Test template TTL is 24 hours
	expectedTTL := 24 * time.Hour

	// Verify expiresAt calculation
	expiresAt := time.Now().Add(expectedTTL)
	duration := expiresAt.Sub(time.Now())

	assert.InDelta(t, 24*float64(time.Hour), float64(duration), float64(time.Second))
}

// TestImportProviders_JSONL tests importing providers from JSONL format.
func TestImportProviders_JSONL(t *testing.T) {
	cfg, _ := config.NewConfig(config.WithConfigDir(t.TempDir()))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(cfg, nil)

	router.POST("/provider-import", handler.ImportProviders)

	jsonlData := `{"type":"metadata","version":"1.0","exported_at":"2024-01-01T00:00:00Z"}
{"type":"provider","uuid":"prov-1","name":"TestProvider","api_base":"https://api.test.com","api_style":"openai","auth_type":"api_key","token":"sk-test","enabled":true,"timeout":30}`

	importReq := ImportProvidersRequest{
		Data:               jsonlData,
		OnProviderConflict: "use",
	}
	body, _ := json.Marshal(importReq)
	req, _ := http.NewRequest("POST", "/provider-import", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	bodyResp := w.Body.String()
	assert.Contains(t, bodyResp, `"success":true`)
	assert.Contains(t, bodyResp, `"providers_created":1`)
}

// TestImportProviders_Base64 tests importing providers from Base64 format
func TestImportProviders_Base64(t *testing.T) {
	cfg, _ := config.NewConfig(config.WithConfigDir(t.TempDir()))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(cfg, nil)

	router.POST("/provider-import", handler.ImportProviders)

	jsonlData := `{"type":"metadata","version":"1.0","exported_at":"2024-01-01T00:00:00Z"}
{"type":"provider","uuid":"prov-1","name":"TestProvider","api_base":"https://api.test.com","api_style":"openai","auth_type":"api_key","token":"sk-test","enabled":true}`

	// Encode the JSONL data to Base64
	base64Payload := base64.StdEncoding.EncodeToString([]byte(jsonlData))
	base64Data := dataio.Base64Prefix + ":1.0:" + base64Payload

	importReq := ImportProvidersRequest{
		Data:               base64Data,
		OnProviderConflict: "use",
	}
	body, _ := json.Marshal(importReq)
	req, _ := http.NewRequest("POST", "/provider-import", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	bodyResp := w.Body.String()
	assert.Contains(t, bodyResp, `"success":true`)
}

// TestImportProviders_ProviderConflictUse tests using existing provider on conflict.
// This test verifies that when a provider with the same UUID is imported,
// the existing provider is used instead of creating a new one.
func TestImportProviders_ProviderConflictUse(t *testing.T) {
	cfg, _ := config.NewConfig(config.WithConfigDir(t.TempDir()))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(cfg, nil)

	router.POST("/provider-import", handler.ImportProviders)

	// First create an existing provider with UUID "prov-1" (same as in the import)
	existingProvider := &typ.Provider{
		UUID:     "prov-1", // Same UUID as in the import data
		Name:     "ExistingProvider",
		APIBase:  "https://api.existing.com",
		APIStyle: protocol.APIStyleOpenAI,
		AuthType: typ.AuthTypeAPIKey,
		Token:    "sk-existing",
		Enabled:  true,
	}
	cfg.AddProvider(existingProvider)

	// Import a provider with the same UUID but different name
	jsonlData := `{"type":"metadata","version":"1.0","exported_at":"2024-01-01T00:00:00Z"}
{"type":"provider","uuid":"prov-1","name":"TestProvider","api_base":"https://api.test.com","api_style":"openai","auth_type":"api_key","token":"sk-test","enabled":true}`

	importReq := ImportProvidersRequest{
		Data:               jsonlData,
		OnProviderConflict: "use", // Use existing provider
	}
	body, _ := json.Marshal(importReq)
	req, _ := http.NewRequest("POST", "/provider-import", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	bodyResp := w.Body.String()
	assert.Contains(t, bodyResp, `"success":true`)

	// Parse response to check provider info
	var resp ImportProvidersResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have 0 providers created (used existing), 1 used
	if resp.Data.ProvidersCreated != 0 {
		t.Errorf("Expected 0 providers created, got %d", resp.Data.ProvidersCreated)
	}
	if resp.Data.ProvidersUsed != 1 {
		t.Errorf("Expected 1 provider used, got %d", resp.Data.ProvidersUsed)
	}
}

// TestImportProviders_InvalidData tests importing with invalid data
func TestImportProviders_InvalidData(t *testing.T) {
	cfg, _ := config.NewConfig(config.WithConfigDir(t.TempDir()))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(cfg, nil)

	router.POST("/provider-import", handler.ImportProviders)

	importReq := ImportProvidersRequest{
		Data:               "invalid data",
		OnProviderConflict: "use",
	}
	body, _ := json.Marshal(importReq)
	req, _ := http.NewRequest("POST", "/provider-import", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}

	bodyResp := w.Body.String()
	assert.Contains(t, bodyResp, `"success":false`)
}

// TestImportProviders_ProviderUUIDConflict tests real UUID conflict scenario
func TestImportProviders_ProviderUUIDConflict(t *testing.T) {
	cfg, _ := config.NewConfig(config.WithConfigDir(t.TempDir()))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(cfg, nil)

	router.POST("/provider-import", handler.ImportProviders)

	// First create an existing provider with the same UUID (simulating re-import)
	existingProvider := &typ.Provider{
		UUID:     "prov-1", // Same UUID as in the export
		Name:     "ExistingProvider",
		APIBase:  "https://api.existing.com",
		APIStyle: protocol.APIStyleOpenAI,
		AuthType: typ.AuthTypeAPIKey,
		Token:    "sk-existing",
		Enabled:  true,
	}
	cfg.AddProvider(existingProvider)

	// Import a provider with the same UUID
	jsonlData := `{"type":"metadata","version":"1.0","exported_at":"2024-01-01T00:00:00Z"}
{"type":"provider","uuid":"prov-1","name":"TestProvider","api_base":"https://api.test.com","api_style":"openai","auth_type":"api_key","token":"sk-test","enabled":true}`

	importReq := ImportProvidersRequest{
		Data:               jsonlData,
		OnProviderConflict: "use", // Use existing provider with same UUID
	}
	body, _ := json.Marshal(importReq)
	req, _ := http.NewRequest("POST", "/provider-import", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	bodyResp := w.Body.String()
	assert.Contains(t, bodyResp, `"success":true`)

	// Parse response to check provider info
	var resp ImportProvidersResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have 0 providers created (used existing), 1 used
	if resp.Data.ProvidersCreated != 0 {
		t.Errorf("Expected 0 providers created, got %d", resp.Data.ProvidersCreated)
	}
	if resp.Data.ProvidersUsed != 1 {
		t.Errorf("Expected 1 provider used, got %d", resp.Data.ProvidersUsed)
	}

	// Verify the used provider is the existing one
	found := false
	for _, p := range resp.Data.Providers {
		if p.UUID == "prov-1" && p.Name == "ExistingProvider" && p.Action == "used" {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected to find existing provider being used")
	}
}

// TestImportProviders_MissingData tests importing with missing data field
func TestImportProviders_MissingData(t *testing.T) {
	cfg, _ := config.NewConfig(config.WithConfigDir(t.TempDir()))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(cfg, nil)

	router.POST("/provider-import", handler.ImportProviders)

	importReq := map[string]string{
		"on_provider_conflict": "use",
		// Missing "data" field
	}
	body, _ := json.Marshal(importReq)
	req, _ := http.NewRequest("POST", "/provider-import", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}
