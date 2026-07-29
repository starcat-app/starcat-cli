package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/starcat-app/starcat-cli/internal/config"
	"github.com/starcat-app/starcat-cli/internal/mcp"
	"github.com/starcat-app/starcat-cli/internal/updater"
)

func TestRepoNoteRejectsDisabledWritesBeforeReadingStdin(t *testing.T) {
	stdin := &recordingReader{}
	runner, calls, closeServer := newWriteRunner(t, stdin, false)
	defer closeServer()

	err := runner.Run(context.Background(), []string{"repo", "note", "set", "toptal/gitignore.io"})
	if err == nil || !strings.Contains(err.Error(), "enable Allow Local Writes") {
		t.Fatalf("Run() error = %v, want local-write guidance", err)
	}
	if stdin.read {
		t.Fatal("stdin was read before local-write capability was checked")
	}
	if calls.upsertNote != 0 {
		t.Fatalf("upsert_repo_note calls = %d, want 0", calls.upsertNote)
	}
}

func TestRepoTagReplaceExplainsDestructiveWriteSetting(t *testing.T) {
	runner, calls, closeServer := newWriteRunner(t, strings.NewReader(""), true)
	defer closeServer()

	err := runner.Run(context.Background(), []string{"repo", "tags", "replace", "toptal/gitignore.io", "agent"})
	if err == nil || !strings.Contains(err.Error(), "enable Allow Local Writes and Allow Replace/Delete Writes") {
		t.Fatalf("Run() error = %v, want destructive-write guidance", err)
	}
	if calls.upsertNote != 0 {
		t.Fatalf("unexpected write tool calls = %d", calls.upsertNote)
	}
}

func TestPairRejectsInvalidURI(t *testing.T) {
	runner := &Runner{Stdin: strings.NewReader("invalid\n"), Stdout: io.Discard, Stderr: io.Discard}
	if err := runner.Run(context.Background(), []string{"pair", "starcat-pair://invalid"}); err == nil || !strings.Contains(err.Error(), "invalid pairing URI") {
		t.Fatalf("pair URI error = %v", err)
	}
}

func TestPairingInputStopsAtEnter(t *testing.T) {
	line, err := readLineContext(context.Background(), strings.NewReader("starcat-pair://first\nsecond"))
	if err != nil {
		t.Fatalf("readLineContext() error = %v", err)
	}
	if line != "starcat-pair://first" {
		t.Fatalf("line = %q, want first line only", line)
	}
}

func TestHelpUsesTerminalTextInsteadOfJSONEnvelope(t *testing.T) {
	var stdout bytes.Buffer
	runner := &Runner{Stdout: &stdout}

	if err := runner.Run(context.Background(), []string{"--help"}); err != nil {
		t.Fatalf("Run(--help) error = %v", err)
	}
	if strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") {
		t.Fatalf("help should not be wrapped in JSON: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Usage:") || !strings.Contains(stdout.String(), "starcat help <command>") {
		t.Fatalf("help output = %q", stdout.String())
	}
}

func TestUpdateWritesTerminalResult(t *testing.T) {
	var stdout bytes.Buffer
	runner := &Runner{
		Stdout: &stdout,
		RunUpdate: func(context.Context, string) (updater.Result, error) {
			return updater.Result{Updated: true, CurrentVersion: "v1.0.0", LatestVersion: "v1.1.0"}, nil
		},
	}
	if err := runner.Run(context.Background(), []string{"update"}); err != nil {
		t.Fatalf("Run(update) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Updated Starcat CLI from v1.0.0 to v1.1.0") {
		t.Fatalf("update stdout = %q, want terminal result", stdout.String())
	}
}

func TestDoctorUsesTerminalText(t *testing.T) {
	var stdout bytes.Buffer
	runner, _, closeServer := newWriteRunner(t, strings.NewReader(""), true)
	defer closeServer()
	runner.Stdout = &stdout

	if err := runner.Run(context.Background(), []string{"doctor"}); err != nil {
		t.Fatalf("Run(doctor) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Starcat Doctor") || strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") {
		t.Fatalf("doctor stdout = %q, want terminal text", stdout.String())
	}

}

func TestUnknownFlagsAreRejected(t *testing.T) {
	runner := &Runner{Stdout: io.Discard}
	err := runner.Run(context.Background(), []string{"repo", "search", "swift", "--semnatic"})
	if err == nil || !strings.Contains(err.Error(), `unknown flag "--semnatic"`) {
		t.Fatalf("Run() error = %v, want unknown flag", err)
	}
}

func TestGlobalSearchMapsSourceAndLimitToMCP(t *testing.T) {
	runner, calls, closeServer := newGlobalSearchRunner(t)
	defer closeServer()
	var stdout bytes.Buffer
	runner.Stdout = &stdout

	err := runner.Run(context.Background(), []string{
		"search", "local RAG", "--source", "local", "--limit", "12",
	})
	if err != nil {
		t.Fatalf("Run(search) error = %v", err)
	}
	if calls.query != "local RAG" || calls.limit != 12 {
		t.Fatalf("search arguments = query %q, limit %d", calls.query, calls.limit)
	}
	if len(calls.sources) != 1 || calls.sources[0] != "local" {
		t.Fatalf("search sources = %#v, want [local]", calls.sources)
	}
	if !strings.Contains(stdout.String(), `"schema_version": 1`) ||
		!strings.Contains(stdout.String(), `"primary_source": "local"`) {
		t.Fatalf("search stdout = %q, want global search JSON", stdout.String())
	}
}

func TestGlobalSearchRejectsInvalidSourceBeforeConnecting(t *testing.T) {
	runner := &Runner{Stdout: io.Discard}
	err := runner.Run(context.Background(), []string{
		"search", "swift", "--source", "web",
	})
	if err == nil || !strings.Contains(err.Error(), "--source must be all, local, or github") {
		t.Fatalf("Run(search) error = %v, want source validation", err)
	}
}

func TestGlobalSearchErrorsExposeStableCode(t *testing.T) {
	testCases := []struct {
		err  error
		code string
	}{
		{err: config.ErrNotPaired, code: "CLI_NOT_PAIRED"},
		{err: mcp.ErrUnauthorized, code: "CLI_NOT_PAIRED"},
		{err: &mcp.ToolError{Code: "REQUIRES_PRO", Message: "wording may change"}, code: "REQUIRES_PRO"},
		{err: mcp.ErrUnavailable, code: "MCP_DISABLED"},
		{err: &mcp.ToolError{Code: "UPGRADE_REQUIRED", Message: "wording may change"}, code: "UPGRADE_REQUIRED"},
	}
	for _, testCase := range testCases {
		classified := classifyGlobalSearchError(testCase.err)
		if !strings.HasPrefix(classified.Error(), "STARCAT_ERROR "+testCase.code+":") {
			t.Fatalf("classified error = %q, want code %s", classified, testCase.code)
		}
	}
}

func TestStatisticsCommandsRenderTerminalFriendlyOutput(t *testing.T) {
	runner, closeServer := newStatisticsRunner(t)
	defer closeServer()
	var stdout bytes.Buffer
	runner.Stdout = &stdout

	if err := runner.Run(context.Background(), []string{"stats"}); err != nil {
		t.Fatalf("Run(stats) error = %v", err)
	}
	if strings.HasPrefix(strings.TrimSpace(stdout.String()), "{") || !strings.Contains(stdout.String(), "Starcat Statistics") {
		t.Fatalf("stats stdout = %q, want terminal overview", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Starred: 12") || !strings.Contains(stdout.String(), "Total tokens: 420") {
		t.Fatalf("stats stdout = %q, want repository and token counts", stdout.String())
	}

	stdout.Reset()
	if err := runner.Run(context.Background(), []string{"stats", "ai", "--range", "30d", "--provider", "provider-a"}); err != nil {
		t.Fatalf("Run(stats ai) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Starcat AI Usage (last 30 days)") || !strings.Contains(stdout.String(), "By provider") {
		t.Fatalf("stats ai stdout = %q", stdout.String())
	}

	stdout.Reset()
	if err := runner.Run(context.Background(), []string{"stats", "knowledge"}); err != nil {
		t.Fatalf("Run(stats knowledge) error = %v", err)
	}
	if !strings.Contains(stdout.String(), "Starcat Knowledge Base Statistics") || !strings.Contains(stdout.String(), "Active chunks: 24") {
		t.Fatalf("stats knowledge stdout = %q", stdout.String())
	}
}

func TestRepoNotePipeInputDoesNotPrintInteractivePrompt(t *testing.T) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	runner, calls, closeServer := newWriteRunner(t, strings.NewReader("pipeline note"), true)
	defer closeServer()
	runner.Stdout = &stdout
	runner.Stderr = &stderr

	if err := runner.Run(context.Background(), []string{"repo", "note", "set", "toptal/gitignore.io", "--apply"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want no prompt for pipe input", stderr.String())
	}
	if calls.noteContent != "pipeline note" || calls.noteDryRun {
		t.Fatalf("note arguments = content %q, dry_run %v", calls.noteContent, calls.noteDryRun)
	}
	if !strings.Contains(stdout.String(), `"changed": true`) {
		t.Fatalf("stdout = %q, want JSON result", stdout.String())
	}
}

func TestRepoNoteInteractiveInputPrintsEOFAndCancelGuidance(t *testing.T) {
	var stderr bytes.Buffer
	runner, _, closeServer := newWriteRunner(t, strings.NewReader("interactive note"), true)
	defer closeServer()
	runner.Stderr = &stderr
	runner.stdinInteractive = true

	if err := runner.Run(context.Background(), []string{"repo", "note", "set", "toptal/gitignore.io"}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if !strings.Contains(stderr.String(), "Ctrl+D") || !strings.Contains(stderr.String(), "Ctrl+C") {
		t.Fatalf("stderr = %q, want EOF and cancellation guidance", stderr.String())
	}
}

func TestRepoNoteStdinReadStopsWhenContextIsCanceled(t *testing.T) {
	stdin := newBlockingReader()
	runner, _, closeServer := newWriteRunner(t, stdin, true)
	defer closeServer()
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan error, 1)
	go func() {
		result <- runner.Run(ctx, []string{"repo", "note", "set", "toptal/gitignore.io"})
	}()

	select {
	case <-stdin.started:
	case <-time.After(time.Second):
		t.Fatal("stdin read did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Run() error = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Run() remained blocked after context cancellation")
	}
	close(stdin.release)
}

type writeCalls struct {
	upsertNote  int
	noteContent string
	noteDryRun  bool
}

type globalSearchCalls struct {
	query   string
	limit   int
	sources []string
}

func newGlobalSearchRunner(t *testing.T) (*Runner, *globalSearchCalls, func()) {
	t.Helper()
	calls := &globalSearchCalls{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var message map[string]any
		if err := json.NewDecoder(request.Body).Decode(&message); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		switch message["method"] {
		case "initialize":
			writeMCPResult(writer, message["id"], map[string]any{"protocolVersion": "2025-03-26"})
		case "notifications/initialized":
			writer.WriteHeader(http.StatusAccepted)
		case "tools/call":
			params, _ := message["params"].(map[string]any)
			tool, _ := params["name"].(string)
			if tool != "starcat.global_search_repos" {
				http.Error(writer, "unexpected tool: "+tool, http.StatusBadRequest)
				return
			}
			arguments, _ := params["arguments"].(map[string]any)
			calls.query, _ = arguments["query"].(string)
			if value, ok := arguments["limit"].(float64); ok {
				calls.limit = int(value)
			}
			for _, source := range arguments["sources"].([]any) {
				calls.sources = append(calls.sources, source.(string))
			}
			writeMCPToolResult(writer, message["id"], map[string]any{
				"schema_version": 1,
				"query":          calls.query,
				"returned_count": 1,
				"items": []any{map[string]any{
					"repo_id": 42, "owner": "openai", "name": "codex", "full_name": "openai/codex",
					"stars_count": 100, "is_private": false, "is_starred": true,
					"primary_source": "local", "sources": []any{"local"},
					"icon_url": "https://github.com/openai.png?size=80",
					"open_url": "starcat://repo/openai/codex?v=1&rid=42",
					"html_url": "https://github.com/openai/codex",
				}},
				"providers": map[string]any{},
				"warnings":  []any{},
			})
		default:
			http.Error(writer, "unexpected MCP method", http.StatusBadRequest)
		}
	}))

	profile := config.Profile{
		Endpoint: server.URL + "/mcp", DeviceID: "device-1", ProtocolVersion: config.CurrentProtocolVersion,
	}
	return &Runner{
		Profiles: staticProfileStore{profile: profile}, Credentials: staticCredentialStore{token: "test-token"},
		Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard,
	}, calls, server.Close
}

func newStatisticsRunner(t *testing.T) (*Runner, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var message map[string]any
		if err := json.NewDecoder(request.Body).Decode(&message); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		switch message["method"] {
		case "initialize":
			writeMCPResult(writer, message["id"], map[string]any{"protocolVersion": "2025-03-26"})
		case "notifications/initialized":
			writer.WriteHeader(http.StatusAccepted)
		case "tools/call":
			params, _ := message["params"].(map[string]any)
			tool, _ := params["name"].(string)
			switch tool {
			case "starcat.get_overview_statistics":
				writeMCPToolResult(writer, message["id"], map[string]any{
					"generated_at": "2026-07-20T00:00:00Z", "starred_repository_count": 12,
					"knowledge_base_project_count": 7, "retained_after_unstar_count": 2, "tag_count": 3,
					"ai_usage_time_range": "all", "ai_usage": statisticsUsageFixture(),
					"rag_index": statisticsRAGFixture(), "excluded_chunk_count": 1,
				})
			case "starcat.get_ai_usage_statistics":
				writeMCPToolResult(writer, message["id"], map[string]any{
					"generated_at": "2026-07-20T00:00:00Z", "time_range": "thirty_days",
					"provider_id": "provider-a", "summary": statisticsUsageFixture(),
					"daily": []any{}, "by_feature": []any{},
					"by_provider": []any{map[string]any{"key": "provider-a", "total_tokens": 420, "call_count": 4}},
					"by_model":    []any{},
				})
			case "starcat.get_knowledge_base_statistics":
				writeMCPToolResult(writer, message["id"], map[string]any{
					"generated_at": "2026-07-20T00:00:00Z", "project_count": 7,
					"starred_project_count": 5, "retained_after_unstar_count": 2,
					"tagged_project_count": 4, "untagged_project_count": 3, "tag_count": 3,
					"known_language_project_count": 6, "unknown_language_project_count": 1,
					"added_in_last_30_days_count": 2, "pushed_in_last_30_days_count": 4,
					"ai_summary_project_count": 3, "private_notes_exposed": false,
					"status_counts": []any{}, "top_languages": []any{}, "top_tags": []any{},
					"source_index_coverage": []any{}, "excluded_chunk_count": 1,
					"without_readme_source_project_count": 1, "without_indexable_source_project_count": 0,
					"top_starred_repositories": []any{}, "index_health": statisticsRAGFixture(),
				})
			default:
				http.Error(writer, "unexpected tool: "+tool, http.StatusBadRequest)
			}
		default:
			http.Error(writer, "unexpected MCP method", http.StatusBadRequest)
		}
	}))

	profile := config.Profile{
		Endpoint: server.URL + "/mcp", DeviceID: "device-1", ProtocolVersion: config.CurrentProtocolVersion,
	}
	return &Runner{
		Profiles: staticProfileStore{profile: profile}, Credentials: staticCredentialStore{token: "test-token"},
		Stdin: strings.NewReader(""), Stdout: io.Discard, Stderr: io.Discard,
	}, server.Close
}

func statisticsUsageFixture() map[string]any {
	return map[string]any{
		"total_tokens": 420, "input_tokens": 300, "output_tokens": 120,
		"call_count": 4, "successful_call_count": 3, "calls_with_usage": 3,
		"embedding_item_count": 8, "success_rate": 0.75, "usage_availability_rate": 0.75,
	}
}

func statisticsRAGFixture() map[string]any {
	return map[string]any{
		"total_chunks": 24, "ready_chunks": 18, "keyword_only_chunks": 2,
		"pending_chunks": 1, "failed_chunks": 1, "stale_chunks": 2, "embedding_model": "embed-v1",
	}
}

func newWriteRunner(t *testing.T, stdin io.Reader, localWrites bool) (*Runner, *writeCalls, func()) {
	t.Helper()
	calls := &writeCalls{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var message map[string]any
		if err := json.NewDecoder(request.Body).Decode(&message); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		switch message["method"] {
		case "initialize":
			writeMCPResult(writer, message["id"], map[string]any{"protocolVersion": "2025-03-26"})
		case "notifications/initialized":
			writer.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writeMCPResult(writer, message["id"], map[string]any{
				"tools": []any{
					map[string]any{"name": "starcat.get_capabilities"},
					map[string]any{"name": "starcat.search_repos"},
				},
			})
		case "tools/call":
			params, _ := message["params"].(map[string]any)
			tool, _ := params["name"].(string)
			arguments, _ := params["arguments"].(map[string]any)
			switch tool {
			case "starcat.get_capabilities":
				writeMCPToolResult(writer, message["id"], map[string]any{
					"local_writes":       localWrites,
					"destructive_writes": false,
				})
			case "starcat.upsert_repo_note":
				calls.upsertNote++
				calls.noteContent, _ = arguments["content"].(string)
				calls.noteDryRun, _ = arguments["dry_run"].(bool)
				writeMCPToolResult(writer, message["id"], map[string]any{"changed": !calls.noteDryRun})
			default:
				http.Error(writer, "unexpected tool: "+tool, http.StatusBadRequest)
			}
		default:
			http.Error(writer, "unexpected MCP method", http.StatusBadRequest)
		}
	}))

	profile := config.Profile{
		Endpoint:        server.URL + "/mcp",
		DeviceID:        "device-1",
		ProtocolVersion: config.CurrentProtocolVersion,
	}
	runner := &Runner{
		Profiles:    staticProfileStore{profile: profile},
		Credentials: staticCredentialStore{token: "test-token"},
		Stdin:       stdin,
		Stdout:      io.Discard,
		Stderr:      io.Discard,
	}
	return runner, calls, server.Close
}

func writeMCPResult(writer http.ResponseWriter, id any, result map[string]any) {
	writer.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}

func writeMCPToolResult(writer http.ResponseWriter, id any, structured map[string]any) {
	writeMCPResult(writer, id, map[string]any{
		"structuredContent": structured,
		"isError":           false,
	})
}

type staticProfileStore struct {
	profile config.Profile
}

func (s staticProfileStore) Load() (config.Profile, error) { return s.profile, nil }
func (s staticProfileStore) Save(config.Profile) error     { return nil }
func (s staticProfileStore) Delete() error                 { return nil }

type staticCredentialStore struct {
	token string
}

func (s staticCredentialStore) Get(string) (string, error) { return s.token, nil }
func (s staticCredentialStore) Set(string, string) error   { return nil }
func (s staticCredentialStore) Delete(string) error        { return nil }

type recordingReader struct {
	read bool
}

func (r *recordingReader) Read([]byte) (int, error) {
	r.read = true
	return 0, errors.New("stdin should not be read")
}

type blockingReader struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func newBlockingReader() *blockingReader {
	return &blockingReader{started: make(chan struct{}), release: make(chan struct{})}
}

func (r *blockingReader) Read([]byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	<-r.release
	return 0, io.EOF
}
