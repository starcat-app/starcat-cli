// Package cli contains terminal renderers for Starcat's read-only statistics tools.
//
// The MCP server remains the single business and JSON contract. These renderers only
// turn the same structured tool results into stable, human-readable terminal output.
package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

type aiUsageSummary struct {
	TotalTokens           int     `json:"total_tokens"`
	InputTokens           int     `json:"input_tokens"`
	OutputTokens          int     `json:"output_tokens"`
	CallCount             int     `json:"call_count"`
	SuccessfulCallCount   int     `json:"successful_call_count"`
	CallsWithUsage        int     `json:"calls_with_usage"`
	EmbeddingItemCount    int     `json:"embedding_item_count"`
	SuccessRate           float64 `json:"success_rate"`
	UsageAvailabilityRate float64 `json:"usage_availability_rate"`
}

type ragIndexHealth struct {
	TotalChunks       int    `json:"total_chunks"`
	ReadyChunks       int    `json:"ready_chunks"`
	KeywordOnlyChunks int    `json:"keyword_only_chunks"`
	PendingChunks     int    `json:"pending_chunks"`
	FailedChunks      int    `json:"failed_chunks"`
	StaleChunks       int    `json:"stale_chunks"`
	EmbeddingModel    string `json:"embedding_model"`
}

type overviewStatistics struct {
	GeneratedAt               string         `json:"generated_at"`
	StarredRepositoryCount    int            `json:"starred_repository_count"`
	KnowledgeBaseProjectCount int            `json:"knowledge_base_project_count"`
	RetainedAfterUnstarCount  int            `json:"retained_after_unstar_count"`
	TagCount                  int            `json:"tag_count"`
	AIUsageTimeRange          string         `json:"ai_usage_time_range"`
	AIUsage                   aiUsageSummary `json:"ai_usage"`
	RAGIndex                  ragIndexHealth `json:"rag_index"`
	ExcludedChunkCount        int            `json:"excluded_chunk_count"`
}

type usageDimension struct {
	Key          string `json:"key"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	TotalTokens  int    `json:"total_tokens"`
	CallCount    int    `json:"call_count"`
}

type usageDailyPoint struct {
	Day          string `json:"day"`
	InputTokens  int    `json:"input_tokens"`
	OutputTokens int    `json:"output_tokens"`
	TotalTokens  int    `json:"total_tokens"`
	CallCount    int    `json:"call_count"`
}

type aiUsageStatistics struct {
	GeneratedAt string            `json:"generated_at"`
	TimeRange   string            `json:"time_range"`
	Feature     *string           `json:"feature"`
	ProviderID  *string           `json:"provider_id"`
	Model       *string           `json:"model"`
	Summary     aiUsageSummary    `json:"summary"`
	Daily       []usageDailyPoint `json:"daily"`
	ByFeature   []usageDimension  `json:"by_feature"`
	ByProvider  []usageDimension  `json:"by_provider"`
	ByModel     []usageDimension  `json:"by_model"`
}

type namedCount struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type sourceCoverage struct {
	Source           string `json:"source"`
	RepositoryCount  int    `json:"repository_count"`
	ChunkCount       int    `json:"chunk_count"`
	ReadyChunkCount  int    `json:"ready_chunk_count"`
	FailedChunkCount int    `json:"failed_chunk_count"`
	StaleChunkCount  int    `json:"stale_chunk_count"`
}

type topRepository struct {
	FullName    string `json:"full_name"`
	GitHubStars int    `json:"github_stars"`
}

type knowledgeBaseStatistics struct {
	GeneratedAt                        string           `json:"generated_at"`
	ContentUpdatedAt                   *string          `json:"content_updated_at"`
	ProjectCount                       int              `json:"project_count"`
	StarredProjectCount                int              `json:"starred_project_count"`
	RetainedAfterUnstarCount           int              `json:"retained_after_unstar_count"`
	StatusCounts                       []namedCount     `json:"status_counts"`
	TaggedProjectCount                 int              `json:"tagged_project_count"`
	UntaggedProjectCount               int              `json:"untagged_project_count"`
	TagCount                           int              `json:"tag_count"`
	KnownLanguageProjectCount          int              `json:"known_language_project_count"`
	UnknownLanguageProjectCount        int              `json:"unknown_language_project_count"`
	TopLanguages                       []namedCount     `json:"top_languages"`
	TopTags                            []namedCount     `json:"top_tags"`
	AddedInLast30DaysCount             int              `json:"added_in_last_30_days_count"`
	PushedInLast30DaysCount            int              `json:"pushed_in_last_30_days_count"`
	AISummaryProjectCount              int              `json:"ai_summary_project_count"`
	PrivateNotesExposed                bool             `json:"private_notes_exposed"`
	PrivateNoteProjectCount            *int             `json:"private_note_project_count"`
	AIGeneratedNoteProjectCount        *int             `json:"ai_generated_note_project_count"`
	SourceIndexCoverage                []sourceCoverage `json:"source_index_coverage"`
	ExcludedChunkCount                 int              `json:"excluded_chunk_count"`
	WithoutReadmeSourceProjectCount    int              `json:"without_readme_source_project_count"`
	WithoutIndexableSourceProjectCount int              `json:"without_indexable_source_project_count"`
	TopStarredRepositories             []topRepository  `json:"top_starred_repositories"`
	IndexHealth                        ragIndexHealth   `json:"index_health"`
}

// runStats calls the same MCP tools used by agents, then renders their structured results for people.
func (r *Runner) runStats(ctx context.Context, args []string) error {
	jsonOutput := false
	filtered := make([]string, 0, len(args))
	for _, argument := range args {
		if argument == "--json" {
			jsonOutput = true
		} else {
			filtered = append(filtered, argument)
		}
	}
	args = filtered
	if len(args) == 0 {
		if jsonOutput {
			return r.call(ctx, "starcat.get_overview_statistics", map[string]any{})
		}
		var value overviewStatistics
		if err := r.callStatistics(ctx, "starcat.get_overview_statistics", map[string]any{}, &value); err != nil {
			return err
		}
		return r.writeText(renderOverviewStatistics(value))
	}

	switch args[0] {
	case "ai":
		positionals, flags, err := parseFlags(args[1:], map[string]bool{
			"range": true, "feature": true, "provider": true, "model": true,
		})
		if err != nil {
			return err
		}
		if len(positionals) != 0 {
			return errors.New("Usage: starcat stats ai [--range today|7d|30d|all] [--feature NAME] [--provider ID] [--model NAME]")
		}
		timeRange, err := normalizeStatsTimeRange(valueFlag(flags, "range", "all"))
		if err != nil {
			return err
		}
		arguments := map[string]any{"time_range": timeRange}
		if value := valueFlag(flags, "feature", ""); value != "" {
			arguments["feature"] = value
		}
		if value := valueFlag(flags, "provider", ""); value != "" {
			arguments["provider_id"] = value
		}
		if value := valueFlag(flags, "model", ""); value != "" {
			arguments["model"] = value
		}
		if jsonOutput {
			return r.call(ctx, "starcat.get_ai_usage_statistics", arguments)
		}
		var value aiUsageStatistics
		if err := r.callStatistics(ctx, "starcat.get_ai_usage_statistics", arguments, &value); err != nil {
			return err
		}
		return r.writeText(renderAIUsageStatistics(value))

	case "knowledge":
		if len(args) != 1 {
			return errors.New("Usage: starcat stats knowledge")
		}
		if jsonOutput {
			return r.call(ctx, "starcat.get_knowledge_base_statistics", map[string]any{})
		}
		var value knowledgeBaseStatistics
		if err := r.callStatistics(ctx, "starcat.get_knowledge_base_statistics", map[string]any{}, &value); err != nil {
			return err
		}
		return r.writeText(renderKnowledgeBaseStatistics(value))

	default:
		return fmt.Errorf("unknown stats subcommand %q; use `starcat help stats` for usage", args[0])
	}
}

func (r *Runner) callStatistics(ctx context.Context, tool string, arguments map[string]any, destination any) error {
	client, err := r.loadClient()
	if err != nil {
		return err
	}
	value, err := client.CallTool(ctx, tool, arguments)
	if err != nil {
		return err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode Starcat statistics: %w", err)
	}
	if err := json.Unmarshal(data, destination); err != nil {
		return fmt.Errorf("decode Starcat statistics: %w", err)
	}
	return nil
}

func normalizeStatsTimeRange(value string) (string, error) {
	switch value {
	case "today", "all":
		return value, nil
	case "7d", "seven_days":
		return "seven_days", nil
	case "30d", "thirty_days":
		return "thirty_days", nil
	default:
		return "", errors.New("--range must be today, 7d, 30d, or all")
	}
}

func renderOverviewStatistics(value overviewStatistics) string {
	return fmt.Sprintf(
		"Starcat Statistics\n\nRepositories\n  Starred: %d\n  Knowledge base: %d\n  Retained after unstar: %d\n  Knowledge-base tags: %d\n\nAI usage (%s)\n%s\n\nRAG index\n%s\n  Excluded chunks: %d\n\nGenerated: %s",
		value.StarredRepositoryCount,
		value.KnowledgeBaseProjectCount,
		value.RetainedAfterUnstarCount,
		value.TagCount,
		humanTimeRange(value.AIUsageTimeRange),
		renderUsageSummary(value.AIUsage),
		renderRAGIndex(value.RAGIndex),
		value.ExcludedChunkCount,
		value.GeneratedAt,
	)
}

func renderAIUsageStatistics(value aiUsageStatistics) string {
	sections := []string{
		fmt.Sprintf("Starcat AI Usage (%s)", humanTimeRange(value.TimeRange)),
		"\n" + renderUsageSummary(value.Summary),
	}
	filters := make([]string, 0, 3)
	if value.Feature != nil {
		filters = append(filters, "feature="+*value.Feature)
	}
	if value.ProviderID != nil {
		filters = append(filters, "provider="+*value.ProviderID)
	}
	if value.Model != nil {
		filters = append(filters, "model="+*value.Model)
	}
	if len(filters) > 0 {
		sections = append(sections, "\nFilters: "+strings.Join(filters, ", "))
	}
	sections = appendDimensionSection(sections, "By feature", value.ByFeature)
	sections = appendDimensionSection(sections, "By provider", value.ByProvider)
	sections = appendDimensionSection(sections, "By model", value.ByModel)
	if len(value.Daily) > 0 {
		lines := []string{"\nDaily"}
		for _, point := range value.Daily {
			lines = append(lines, fmt.Sprintf("  %-10s  %d tokens  %d calls", point.Day, point.TotalTokens, point.CallCount))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	sections = append(sections, "\nGenerated: "+value.GeneratedAt)
	return strings.Join(sections, "\n")
}

func renderKnowledgeBaseStatistics(value knowledgeBaseStatistics) string {
	sections := []string{
		"Starcat Knowledge Base Statistics",
		fmt.Sprintf(
			"\nProjects\n  Total: %d\n  Still starred: %d\n  Retained after unstar: %d\n  Tagged / untagged: %d / %d\n  Tags: %d\n  Known / unknown language: %d / %d",
			value.ProjectCount,
			value.StarredProjectCount,
			value.RetainedAfterUnstarCount,
			value.TaggedProjectCount,
			value.UntaggedProjectCount,
			value.TagCount,
			value.KnownLanguageProjectCount,
			value.UnknownLanguageProjectCount,
		),
		fmt.Sprintf(
			"\nRecent activity (30 days)\n  Added to knowledge base: %d\n  Pushed on GitHub: %d\n  Repositories with AI summaries: %d",
			value.AddedInLast30DaysCount,
			value.PushedInLast30DaysCount,
			value.AISummaryProjectCount,
		),
	}
	sections = appendNamedCountSection(sections, "Status", value.StatusCounts)
	sections = appendNamedCountSection(sections, "Top languages", value.TopLanguages)
	sections = appendNamedCountSection(sections, "Top tags", value.TopTags)
	if value.PrivateNotesExposed && value.PrivateNoteProjectCount != nil {
		sections = append(sections, fmt.Sprintf(
			"\nPrivate notes\n  Repositories with notes: %d\n  AI-generated notes: %d",
			*value.PrivateNoteProjectCount,
			valueOrZero(value.AIGeneratedNoteProjectCount),
		))
	} else {
		sections = append(sections, "\nPrivate notes\n  Hidden by Starcat MCP privacy settings")
	}
	if len(value.SourceIndexCoverage) > 0 {
		lines := []string{"\nIndexed sources"}
		for _, source := range value.SourceIndexCoverage {
			lines = append(lines, fmt.Sprintf(
				"  %-16s  %d repos  %d active  %d ready  %d failed  %d stale",
				source.Source,
				source.RepositoryCount,
				source.ChunkCount,
				source.ReadyChunkCount,
				source.FailedChunkCount,
				source.StaleChunkCount,
			))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	sections = append(sections, fmt.Sprintf(
		"\nRAG index\n%s\n  Excluded chunks: %d\n  Projects without README source: %d\n  Projects without indexable source: %d",
		renderRAGIndex(value.IndexHealth),
		value.ExcludedChunkCount,
		value.WithoutReadmeSourceProjectCount,
		value.WithoutIndexableSourceProjectCount,
	))
	if len(value.TopStarredRepositories) > 0 {
		lines := []string{"\nTop repositories by GitHub stars"}
		for _, repository := range value.TopStarredRepositories {
			lines = append(lines, fmt.Sprintf("  %-40s  %d", repository.FullName, repository.GitHubStars))
		}
		sections = append(sections, strings.Join(lines, "\n"))
	}
	sections = append(sections, "\nGenerated: "+value.GeneratedAt)
	return strings.Join(sections, "\n")
}

func renderUsageSummary(value aiUsageSummary) string {
	return fmt.Sprintf(
		"  Total tokens: %d\n  Input / output: %d / %d\n  Calls: %d (%d successful)\n  Usage reported: %d/%d (%.1f%%)\n  Embedding items: %d",
		value.TotalTokens,
		value.InputTokens,
		value.OutputTokens,
		value.CallCount,
		value.SuccessfulCallCount,
		value.CallsWithUsage,
		value.CallCount,
		value.UsageAvailabilityRate*100,
		value.EmbeddingItemCount,
	)
}

func renderRAGIndex(value ragIndexHealth) string {
	return fmt.Sprintf(
		"  Active chunks: %d\n  Vector-ready / keyword-only: %d / %d\n  Pending / failed / stale: %d / %d / %d\n  Embedding model: %s",
		value.TotalChunks,
		value.ReadyChunks,
		value.KeywordOnlyChunks,
		value.PendingChunks,
		value.FailedChunks,
		value.StaleChunks,
		value.EmbeddingModel,
	)
}

func appendDimensionSection(sections []string, title string, values []usageDimension) []string {
	if len(values) == 0 {
		return sections
	}
	lines := []string{"\n" + title}
	for _, value := range values {
		lines = append(lines, fmt.Sprintf("  %-24s  %d tokens  %d calls", value.Key, value.TotalTokens, value.CallCount))
	}
	return append(sections, strings.Join(lines, "\n"))
}

func appendNamedCountSection(sections []string, title string, values []namedCount) []string {
	if len(values) == 0 {
		return sections
	}
	sort.SliceStable(values, func(left, right int) bool {
		if values[left].Count == values[right].Count {
			return values[left].Name < values[right].Name
		}
		return values[left].Count > values[right].Count
	})
	lines := []string{"\n" + title}
	for _, value := range values {
		lines = append(lines, fmt.Sprintf("  %-24s  %d", value.Name, value.Count))
	}
	return append(sections, strings.Join(lines, "\n"))
}

func humanTimeRange(value string) string {
	switch value {
	case "today":
		return "today"
	case "seven_days":
		return "last 7 days"
	case "thirty_days":
		return "last 30 days"
	case "all":
		return "all time"
	default:
		return value
	}
}

func valueOrZero(value *int) int {
	if value == nil {
		return 0
	}
	return *value
}
