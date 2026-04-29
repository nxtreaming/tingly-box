package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestApplyClaudeSettings_NewFile(t *testing.T) {
	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "tingly-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Override UserHomeDir for this test
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	// Create .claude directory
	claudeDir := filepath.Join(tempDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("Failed to create .claude dir: %v", err)
	}

	result, err := ApplyClaudeSettingsFromEnv(map[string]string{
		"ANTHROPIC_MODEL":    "test-model",
		"ANTHROPIC_BASE_URL": "http://localhost:12580",
	})
	if err != nil {
		t.Fatalf("ApplyClaudeSettings failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got failure: %s", result.Message)
	}

	if !result.Created {
		t.Errorf("Expected Created to be true for new file")
	}

	if result.BackupPath != "" {
		t.Errorf("Expected no backup path for new file, got: %s", result.BackupPath)
	}

	// Verify file was created
	targetPath := filepath.Join(claudeDir, "settings.json")
	if _, err := os.Stat(targetPath); os.IsNotExist(err) {
		t.Errorf("Expected file to be created at %s", targetPath)
	}

	// Verify content
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("Failed to read created file: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	env, ok := config["env"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected env section in config")
	}

	if env["ANTHROPIC_MODEL"] != "test-model" {
		t.Errorf("Expected ANTHROPIC_MODEL to be 'test-model', got: %v", env["ANTHROPIC_MODEL"])
	}
}

func TestApplyClaudeSettings_ExistingFile(t *testing.T) {
	// Create a temporary directory
	tempDir, err := os.MkdirTemp("", "tingly-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Override UserHomeDir for this test
	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	// Create .claude directory and existing settings.json
	claudeDir := filepath.Join(tempDir, ".claude")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatalf("Failed to create .claude dir: %v", err)
	}

	existingConfig := map[string]interface{}{
		"someKey": "someValue",
		"env": map[string]string{
			"OLD_KEY": "old_value",
		},
	}
	existingData, _ := json.MarshalIndent(existingConfig, "", "  ")
	targetPath := filepath.Join(claudeDir, "settings.json")
	if err := os.WriteFile(targetPath, existingData, 0644); err != nil {
		t.Fatalf("Failed to create existing file: %v", err)
	}

	result, err := ApplyClaudeSettingsFromEnv(map[string]string{
		"ANTHROPIC_MODEL":    "test-model",
		"ANTHROPIC_BASE_URL": "http://localhost:12580",
	})
	if err != nil {
		t.Fatalf("ApplyClaudeSettings failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got failure: %s", result.Message)
	}

	if !result.Updated {
		t.Errorf("Expected Updated to be true for existing file")
	}

	if result.BackupPath == "" {
		t.Errorf("Expected backup path for existing file")
	}

	// Verify backup was created
	if _, err := os.Stat(result.BackupPath); os.IsNotExist(err) {
		t.Errorf("Expected backup file to be created at %s", result.BackupPath)
	}

	// Verify content - env should be replaced, other keys preserved
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("Failed to read updated file: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	// Check that someKey is preserved
	if config["someKey"] != "someValue" {
		t.Errorf("Expected someKey to be preserved")
	}

	// Check that env was replaced with the test values
	env, ok := config["env"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected env section in config")
	}

	if env["ANTHROPIC_MODEL"] != "test-model" {
		t.Errorf("Expected ANTHROPIC_MODEL to be 'test-model', got: %v", env["ANTHROPIC_MODEL"])
	}
}

func TestApplyClaudeOnboarding_NewFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	payload := map[string]interface{}{
		"hasCompletedOnboarding": true,
	}

	result, err := ApplyClaudeOnboarding(payload)
	if err != nil {
		t.Fatalf("ApplyClaudeOnboarding failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got failure: %s", result.Message)
	}

	if !result.Created {
		t.Errorf("Expected Created to be true")
	}
}

func TestApplyClaudeOnboarding_ExistingFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	// Create existing .claude.json
	existingConfig := map[string]interface{}{
		"someKey":      "preserved",
		"otherSetting": 123,
	}
	existingData, _ := json.MarshalIndent(existingConfig, "", "  ")
	targetPath := filepath.Join(tempDir, ".claude.json")
	if err := os.WriteFile(targetPath, existingData, 0644); err != nil {
		t.Fatalf("Failed to create existing file: %v", err)
	}

	payload := map[string]interface{}{
		"hasCompletedOnboarding": true,
	}

	result, err := ApplyClaudeOnboarding(payload)
	if err != nil {
		t.Fatalf("ApplyClaudeOnboarding failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got failure: %s", result.Message)
	}

	if !result.Updated {
		t.Errorf("Expected Updated to be true")
	}

	if result.BackupPath == "" {
		t.Errorf("Expected backup path")
	}

	// Verify existing keys are preserved
	data, _ := os.ReadFile(targetPath)
	var config map[string]interface{}
	json.Unmarshal(data, &config)

	if config["someKey"] != "preserved" {
		t.Errorf("Expected someKey to be preserved")
	}

	if config["hasCompletedOnboarding"] != true {
		t.Errorf("Expected hasCompletedOnboarding to be true")
	}
}

func TestApplyOpenCodeConfig_NewFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	payload := map[string]interface{}{
		"provider": map[string]interface{}{
			"tingly-box": map[string]interface{}{
				"name": "tingly-box",
				"npm":  "@ai-sdk/anthropic",
				"options": map[string]interface{}{
					"baseURL": "http://localhost:12580/tingly/opencode",
				},
				"models": map[string]interface{}{
					"test-model": map[string]interface{}{
						"name": "test-model",
					},
				},
			},
		},
	}

	result, err := ApplyOpenCodeConfig(payload)
	if err != nil {
		t.Fatalf("ApplyOpenCodeConfig failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got failure: %s", result.Message)
	}

	if !result.Created {
		t.Errorf("Expected Created to be true")
	}
}

func TestApplyOpenCodeConfig_ExistingFile(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	originalHome := os.Getenv("HOME")
	os.Setenv("HOME", tempDir)
	defer os.Setenv("HOME", originalHome)

	// Create existing config directory and file
	configDir := filepath.Join(tempDir, ".config", "opencode")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("Failed to create config dir: %v", err)
	}

	existingConfig := map[string]interface{}{
		"$schema": "https://opencode.ai/config.json",
		"provider": map[string]interface{}{
			"other-provider": map[string]interface{}{
				"name": "other-provider",
			},
		},
	}
	existingData, _ := json.MarshalIndent(existingConfig, "", "  ")
	targetPath := filepath.Join(configDir, "opencode.json")
	if err := os.WriteFile(targetPath, existingData, 0644); err != nil {
		t.Fatalf("Failed to create existing file: %v", err)
	}

	payload := map[string]interface{}{
		"provider": map[string]interface{}{
			"tingly-box": map[string]interface{}{
				"name": "tingly-box",
				"npm":  "@ai-sdk/anthropic",
			},
		},
	}

	result, err := ApplyOpenCodeConfig(payload)
	if err != nil {
		t.Fatalf("ApplyOpenCodeConfig failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got failure: %s", result.Message)
	}

	if !result.Updated {
		t.Errorf("Expected Updated to be true")
	}

	// Verify other provider is preserved and tingly-box is added
	data, _ := os.ReadFile(targetPath)
	var config map[string]interface{}
	json.Unmarshal(data, &config)

	providers, ok := config["provider"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected provider section")
	}

	if providers["other-provider"] == nil {
		t.Errorf("Expected other-provider to be preserved")
	}

	if providers["tingly-box"] == nil {
		t.Errorf("Expected tingly-box to be added")
	}
}

func TestBackupFileNaming(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create a test file
	testFile := filepath.Join(tempDir, "test.json")
	if err := os.WriteFile(testFile, []byte("{}"), 0644); err != nil {
		t.Fatalf("Failed to create test file: %v", err)
	}

	// Generate backup path
	backupPath := generateBackupPath(testFile)

	// Verify it contains timestamp
	expectedSuffix := ".json.bak-" + time.Now().Format("20060102-")
	if len(backupPath) < len(expectedSuffix) {
		t.Errorf("Backup path too short: %s", backupPath)
	}

	// Verify backup doesn't exist yet
	if _, err := os.Stat(backupPath); !os.IsNotExist(err) {
		t.Errorf("Backup should not exist yet: %s", backupPath)
	}
}

func TestEnsureDir(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	nestedPath := filepath.Join(tempDir, "a", "b", "c", "file.json")

	// Ensure directory exists
	if err := ensureDir(nestedPath); err != nil {
		t.Fatalf("ensureDir failed: %v", err)
	}

	// Verify directory was created
	if _, err := os.Stat(filepath.Dir(nestedPath)); os.IsNotExist(err) {
		t.Errorf("Expected directory to be created")
	}
}

func TestApplyClaudeSettingsToPath_WithBackupDisabled(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create existing file
	targetPath := filepath.Join(tempDir, "settings.json")
	existingConfig := map[string]interface{}{
		"someKey": "someValue",
	}
	existingData, _ := json.MarshalIndent(existingConfig, "", "  ")
	if err := os.WriteFile(targetPath, existingData, 0644); err != nil {
		t.Fatalf("Failed to create existing file: %v", err)
	}

	// Apply with backup disabled
	result, err := ApplyClaudeSettingsToPath(targetPath, map[string]string{
		"ANTHROPIC_MODEL": "test-model",
	}, WithBackup(false))
	if err != nil {
		t.Fatalf("ApplyClaudeSettingsToPath failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got failure: %s", result.Message)
	}

	// Verify no backup was created
	if result.BackupPath != "" {
		t.Errorf("Expected no backup when disabled, got: %s", result.BackupPath)
	}

	backupDir := filepath.Join(filepath.Dir(targetPath), "backup")
	entries, _ := os.ReadDir(backupDir)
	if len(entries) > 0 {
		t.Errorf("Expected backup directory to be empty, found %d entries", len(entries))
	}
}

func TestApplyClaudeSettingsToPath_WithBackupEnabled(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create existing file
	targetPath := filepath.Join(tempDir, "settings.json")
	existingConfig := map[string]interface{}{
		"someKey": "someValue",
	}
	existingData, _ := json.MarshalIndent(existingConfig, "", "  ")
	if err := os.WriteFile(targetPath, existingData, 0644); err != nil {
		t.Fatalf("Failed to create existing file: %v", err)
	}

	// Apply with backup enabled (default)
	result, err := ApplyClaudeSettingsToPath(targetPath, map[string]string{
		"ANTHROPIC_MODEL": "test-model",
	}, WithBackup(true))
	if err != nil {
		t.Fatalf("ApplyClaudeSettingsToPath failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got failure: %s", result.Message)
	}

	// Verify backup was created
	if result.BackupPath == "" {
		t.Errorf("Expected backup path when enabled")
	}

	if _, err := os.Stat(result.BackupPath); os.IsNotExist(err) {
		t.Errorf("Expected backup file to exist at %s", result.BackupPath)
	}
}

func TestApplyClaudeSettingsToPath_WithExtra(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	targetPath := filepath.Join(tempDir, "settings.json")

	// Apply with extra statusLine config
	statusLine := map[string]any{
		"type":    "command",
		"command": "/path/to/script.sh",
	}
	result, err := ApplyClaudeSettingsToPath(targetPath, map[string]string{
		"ANTHROPIC_MODEL": "test-model",
	}, WithExtra("statusLine", statusLine), WithBackup(false))
	if err != nil {
		t.Fatalf("ApplyClaudeSettingsToPath failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got failure: %s", result.Message)
	}

	// Verify statusLine was added
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	sl, ok := config["statusLine"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected statusLine section in config")
	}

	if sl["type"] != "command" {
		t.Errorf("Expected statusLine type to be 'command', got: %v", sl["type"])
	}

	if sl["command"] != "/path/to/script.sh" {
		t.Errorf("Expected statusLine command to be '/path/to/script.sh', got: %v", sl["command"])
	}
}

func TestApplyClaudeSettingsToPath_MultipleWithExtra(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	targetPath := filepath.Join(tempDir, "settings.json")

	// Apply with multiple extras using multiple WithExtra calls
	result, err := ApplyClaudeSettingsToPath(targetPath, map[string]string{
		"ANTHROPIC_MODEL": "test-model",
	},
		WithExtra("key1", "value1"),
		WithExtra("key2", "value2"),
		WithExtra("statusLine", map[string]any{
			"type":    "command",
			"command": "/path/to/script.sh",
		}),
		WithBackup(false),
	)
	if err != nil {
		t.Fatalf("ApplyClaudeSettingsToPath failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got failure: %s", result.Message)
	}

	// Verify all extras were added
	data, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}

	var config map[string]interface{}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("Failed to parse JSON: %v", err)
	}

	if config["key1"] != "value1" {
		t.Errorf("Expected key1 to be 'value1', got: %v", config["key1"])
	}
	if config["key2"] != "value2" {
		t.Errorf("Expected key2 to be 'value2', got: %v", config["key2"])
	}

	sl, ok := config["statusLine"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected statusLine section in config")
	}

	if sl["command"] != "/path/to/script.sh" {
		t.Errorf("Expected statusLine command to be '/path/to/script.sh', got: %v", sl["command"])
	}
}

// writeFakeBackup synthesizes a backup file at <dir>/backup/ with a controlled
// timestamp embedded in its filename. Used by rotation tests to avoid the
// 1-second granularity of the real timestamping path.
func writeFakeBackup(t *testing.T, originalPath string, ts time.Time, content string) string {
	t.Helper()
	dir := filepath.Dir(originalPath)
	base := filepath.Base(originalPath)
	ext := filepath.Ext(originalPath)
	backupDir := filepath.Join(dir, "backup")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		t.Fatalf("mkdir backup: %v", err)
	}
	name := fmt.Sprintf("%s.bak-%s%s", base, ts.Format(backupTimestampLayout), ext)
	path := filepath.Join(backupDir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write fake backup: %v", err)
	}
	return path
}

func TestBackupRotation_KeepsLatestN(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	targetPath := filepath.Join(tempDir, "settings.json")
	if err := os.WriteFile(targetPath, []byte(`{}`), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// Synthesize 6 backups spaced 10 seconds apart.
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.Local)
	var paths []string
	for i := 0; i < 6; i++ {
		paths = append(paths, writeFakeBackup(t, targetPath, base.Add(time.Duration(i)*10*time.Second), fmt.Sprintf("v%d", i)))
	}

	if err := rotateBackups(targetPath, 3); err != nil {
		t.Fatalf("rotateBackups: %v", err)
	}

	backups, err := ListBackups(targetPath)
	if err != nil {
		t.Fatalf("ListBackups: %v", err)
	}
	if len(backups) != 3 {
		t.Fatalf("Expected 3 backups after rotation, got %d", len(backups))
	}
	// The 3 newest must be the last 3 we created.
	expected := map[string]bool{paths[3]: true, paths[4]: true, paths[5]: true}
	for _, b := range backups {
		if !expected[b.Path] {
			t.Errorf("Unexpected backup retained: %s", b.Path)
		}
	}
	// Order is newest-first.
	for i := 1; i < len(backups); i++ {
		if !backups[i-1].Timestamp.After(backups[i].Timestamp) {
			t.Errorf("Backups not in descending order")
		}
	}
}

func TestBackupRotation_DoesNotTouchOtherBaseFiles(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	settings := filepath.Join(tempDir, "settings.json")
	other := filepath.Join(tempDir, "other.json")
	if err := os.WriteFile(settings, []byte(`{}`), 0644); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	if err := os.WriteFile(other, []byte(`{}`), 0644); err != nil {
		t.Fatalf("seed other: %v", err)
	}

	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.Local)
	otherBackup := writeFakeBackup(t, other, base, "x")
	for i := 0; i < 5; i++ {
		writeFakeBackup(t, settings, base.Add(time.Duration(i)*10*time.Second), fmt.Sprintf("s%d", i))
	}

	if err := rotateBackups(settings, defaultBackupRetention); err != nil {
		t.Fatalf("rotateBackups: %v", err)
	}

	if _, err := os.Stat(otherBackup); err != nil {
		t.Errorf("Rotation of settings.json removed unrelated backup %s: %v", otherBackup, err)
	}

	settingsBackups, err := ListBackups(settings)
	if err != nil {
		t.Fatalf("ListBackups(settings): %v", err)
	}
	if len(settingsBackups) != defaultBackupRetention {
		t.Errorf("Expected %d settings backups, got %d", defaultBackupRetention, len(settingsBackups))
	}
}

func TestRestoreLatestBackup_RoundTrip(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	targetPath := filepath.Join(tempDir, "settings.json")
	original := []byte(`{"version":"original"}`)
	mutated := []byte(`{"version":"mutated"}`)

	// Seed a synthetic backup that holds the "original" content.
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.Local)
	backupPath := writeFakeBackup(t, targetPath, base, string(original))

	// Live file holds the mutated state we want to undo.
	if err := os.WriteFile(targetPath, mutated, 0644); err != nil {
		t.Fatalf("seed live: %v", err)
	}

	result, err := RestoreLatestBackup(targetPath)
	if err != nil {
		t.Fatalf("RestoreLatestBackup: %v", err)
	}
	if !result.Success {
		t.Fatalf("Restore not successful: %s", result.Message)
	}
	if result.RestoredFrom != backupPath {
		t.Errorf("Restored from %q, want %q", result.RestoredFrom, backupPath)
	}
	if result.PreRestoreBackup == "" {
		t.Errorf("Expected a pre-restore backup to be created")
	}

	got, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read restored: %v", err)
	}
	if string(got) != string(original) {
		t.Errorf("Restored content mismatch: got %s, want %s", got, original)
	}

	preData, err := os.ReadFile(result.PreRestoreBackup)
	if err != nil {
		t.Fatalf("read pre-restore backup: %v", err)
	}
	if string(preData) != string(mutated) {
		t.Errorf("Pre-restore backup mismatch: got %s, want %s", preData, mutated)
	}
}

func TestRestoreLatestBackup_NoBackup(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	targetPath := filepath.Join(tempDir, "settings.json")
	if err := os.WriteFile(targetPath, []byte(`{}`), 0644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	result, err := RestoreLatestBackup(targetPath)
	if err == nil {
		t.Fatalf("Expected error when no backup exists, got nil")
	}
	if result == nil || result.Success {
		t.Errorf("Expected unsuccessful result, got %+v", result)
	}
}

func TestListBackups_MissingDir(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	backups, err := ListBackups(filepath.Join(tempDir, "settings.json"))
	if err != nil {
		t.Fatalf("ListBackups should not error on missing backup dir: %v", err)
	}
	if len(backups) != 0 {
		t.Errorf("Expected no backups, got %d", len(backups))
	}
}

func TestApplyClaudeSettingsToPath_DefaultBackupBehavior(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "tingly-test")
	if err != nil {
		t.Fatalf("Failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// Create existing file
	targetPath := filepath.Join(tempDir, "settings.json")
	existingConfig := map[string]interface{}{
		"someKey": "someValue",
	}
	existingData, _ := json.MarshalIndent(existingConfig, "", "  ")
	if err := os.WriteFile(targetPath, existingData, 0644); err != nil {
		t.Fatalf("Failed to create existing file: %v", err)
	}

	// Apply without specifying backup option (should default to true)
	result, err := ApplyClaudeSettingsToPath(targetPath, map[string]string{
		"ANTHROPIC_MODEL": "test-model",
	})
	if err != nil {
		t.Fatalf("ApplyClaudeSettingsToPath failed: %v", err)
	}

	if !result.Success {
		t.Errorf("Expected success, got failure: %s", result.Message)
	}

	// Verify backup was created by default
	if result.BackupPath == "" {
		t.Errorf("Expected backup path by default")
	}

	if _, err := os.Stat(result.BackupPath); os.IsNotExist(err) {
		t.Errorf("Expected backup file to exist at %s by default", result.BackupPath)
	}
}
