package bot

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestExpandPathFrom_Absolute(t *testing.T) {
	got, err := ExpandPathFrom("/tmp/foo", "/home/me/proj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "/tmp/foo" {
		t.Fatalf("absolute path mutated: %q", got)
	}
}

func TestExpandPathFrom_RelativeUsesBaseDir(t *testing.T) {
	got, err := ExpandPathFrom("src/api", "/home/me/proj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Clean("/home/me/proj/src/api")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExpandPathFrom_RelativeDotDotEscape(t *testing.T) {
	got, err := ExpandPathFrom("../sibling", "/home/me/proj")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Clean("/home/me/sibling")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExpandPathFrom_RelativeWithoutBaseFallsBackToCwd(t *testing.T) {
	got, err := ExpandPathFrom(".", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("expected absolute fallback, got %q", got)
	}
}

func TestExpandPathFrom_HomeAlias(t *testing.T) {
	got, err := ExpandPathFrom("~/x", "/whatever")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasSuffix(got, "/x") || strings.HasPrefix(got, "/whatever/") {
		t.Fatalf("home alias not honored: %q", got)
	}
}

func TestParsePositiveInt(t *testing.T) {
	tests := []struct {
		in     string
		want   int
		wantOk bool
	}{
		{"1", 1, true},
		{"42", 42, true},
		{"  7  ", 7, true},
		{"0", 0, false},
		{"", 0, false},
		{"-3", 0, false},
		{"3a", 0, false},
		{"abc", 0, false},
	}
	for _, tt := range tests {
		got, ok := parsePositiveInt(tt.in)
		if got != tt.want || ok != tt.wantOk {
			t.Errorf("parsePositiveInt(%q) = (%d,%v), want (%d,%v)", tt.in, got, ok, tt.want, tt.wantOk)
		}
	}
}

func TestBuildFooter_BothFields(t *testing.T) {
	got := BuildFooter(AgentNameClaude, "/home/me/proj")
	if !strings.Contains(got, SeparatorLine) {
		t.Errorf("missing separator: %q", got)
	}
	if !strings.Contains(got, AgentNameCC) {
		t.Errorf("missing agent name: %q", got)
	}
	if !strings.Contains(got, "proj") {
		t.Errorf("missing project path: %q", got)
	}
}

func TestBuildFooter_EmptyReturnsEmpty(t *testing.T) {
	if got := BuildFooter("", ""); got != "" {
		t.Errorf("expected empty footer, got %q", got)
	}
}

func TestBuildFooter_OnlyProject(t *testing.T) {
	got := BuildFooter("", "/home/me/proj")
	if !strings.Contains(got, "proj") {
		t.Errorf("missing project: %q", got)
	}
	if strings.Contains(got, AgentNameCC) || strings.Contains(got, AgentNameTB) {
		t.Errorf("agent line should not appear: %q", got)
	}
}

func TestPushProjectHistory_PrependsAndDedupes(t *testing.T) {
	chat := &Chat{}
	pushProjectHistory(chat, "/a")
	pushProjectHistory(chat, "/b")
	pushProjectHistory(chat, "/c")
	pushProjectHistory(chat, "/a") // dedupe — should move to front, not duplicate
	want := []string{"/a", "/c", "/b"}
	if len(chat.ProjectHistory) != len(want) {
		t.Fatalf("history length %d, want %d (%v)", len(chat.ProjectHistory), len(want), chat.ProjectHistory)
	}
	for i, w := range want {
		if chat.ProjectHistory[i] != w {
			t.Errorf("history[%d] = %q, want %q (%v)", i, chat.ProjectHistory[i], w, chat.ProjectHistory)
		}
	}
	if chat.ProjectPath != "/a" {
		t.Errorf("ProjectPath = %q, want /a", chat.ProjectPath)
	}
}

func TestPushProjectHistory_SeedsLegacyProjectPath(t *testing.T) {
	chat := &Chat{ProjectPath: "/legacy"} // pre-existing binding from before history
	pushProjectHistory(chat, "/new")
	want := []string{"/new", "/legacy"}
	if len(chat.ProjectHistory) != 2 || chat.ProjectHistory[0] != want[0] || chat.ProjectHistory[1] != want[1] {
		t.Errorf("history = %v, want %v", chat.ProjectHistory, want)
	}
}

func TestPushProjectHistory_EmptyPathIsNoOp(t *testing.T) {
	chat := &Chat{ProjectPath: "/x", ProjectHistory: []string{"/x"}}
	pushProjectHistory(chat, "")
	if chat.ProjectPath != "/x" || len(chat.ProjectHistory) != 1 {
		t.Errorf("empty path should not mutate state: path=%q history=%v", chat.ProjectPath, chat.ProjectHistory)
	}
}

func TestPushProjectHistory_Caps(t *testing.T) {
	chat := &Chat{}
	for i := 0; i < projectHistoryCap+5; i++ {
		pushProjectHistory(chat, fmt.Sprintf("/p%d", i))
	}
	if len(chat.ProjectHistory) != projectHistoryCap {
		t.Errorf("history not capped: got %d, want %d", len(chat.ProjectHistory), projectHistoryCap)
	}
}

func TestListChatProjectPaths_FallbackToProjectPath(t *testing.T) {
	dir := t.TempDir()
	store, err := NewChatStoreJSON(dir + "/chats.json")
	if err != nil {
		t.Fatalf("NewChatStoreJSON: %v", err)
	}
	defer store.Close()
	// Simulate a legacy chat written before ProjectHistory existed.
	if err := store.UpsertChat(&Chat{
		ChatID:      "legacy",
		Platform:    "telegram",
		ProjectPath: "/legacy/path",
	}); err != nil {
		t.Fatalf("UpsertChat: %v", err)
	}
	got, err := store.ListChatProjectPaths("legacy")
	if err != nil {
		t.Fatalf("ListChatProjectPaths: %v", err)
	}
	if len(got) != 1 || got[0] != "/legacy/path" {
		t.Errorf("got %v, want [/legacy/path]", got)
	}
}

func TestBindProject_RecordsHistoryPerChat(t *testing.T) {
	dir := t.TempDir()
	store, err := NewChatStoreJSON(dir + "/chats.json")
	if err != nil {
		t.Fatalf("NewChatStoreJSON: %v", err)
	}
	defer store.Close()
	if err := store.BindProject("c1", "telegram", "/a", "alice"); err != nil {
		t.Fatalf("BindProject /a: %v", err)
	}
	if err := store.BindProject("c1", "telegram", "/b", "alice"); err != nil {
		t.Fatalf("BindProject /b: %v", err)
	}
	if err := store.BindProject("c1", "telegram", "/a", "alice"); err != nil {
		t.Fatalf("BindProject /a (re-bind): %v", err)
	}
	got, err := store.ListChatProjectPaths("c1")
	if err != nil {
		t.Fatalf("ListChatProjectPaths: %v", err)
	}
	want := []string{"/a", "/b"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("got %v, want %v", got, want)
	}
}
