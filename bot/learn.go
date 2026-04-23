package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const learnBaseURL = "https://learn.biswas.me"

type learnResult struct {
	CourseTitle  string  `json:"course_title"`
	PageTitle   string  `json:"page_title"`
	SectionTitle string `json:"section_title"`
	Snippet     string  `json:"snippet"`
	Score       float64 `json:"score"`
	URL         string  `json:"url"`
	CourseSlug  string  `json:"course_slug"`
}

type learnSearchResp struct {
	Query   string         `json:"query"`
	Results []learnResult  `json:"results"`
	Total   int            `json:"total"`
}

func searchLearn(ctx context.Context, apiKey, query string, limit int) (*learnSearchResp, error) {
	if limit <= 0 {
		limit = 5
	}
	u := fmt.Sprintf("%s/api/search/semantic?q=%s&limit=%d", learnBaseURL, url.QueryEscape(query), limit)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "ApiKey "+apiKey)

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// Retry once on 503 (learn server occasionally restarts)
	if resp.StatusCode == 503 {
		resp.Body.Close()
		time.Sleep(2 * time.Second)
		req2, _ := http.NewRequestWithContext(ctx, "GET", u, nil)
		req2.Header.Set("Authorization", "ApiKey "+apiKey)
		resp, err = client.Do(req2)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("learn API returned %d", resp.StatusCode)
	}

	var result learnSearchResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &result, nil
}

// formatLearnResults builds a Telegram-friendly plain text response with
// clickable links and snippets.
func formatLearnResults(query string, results []learnResult) string {
	if len(results) == 0 {
		return fmt.Sprintf("No results found for '%s' on learn.biswas.me.", query)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "📚 learn.biswas.me — \"%s\"\n", query)
	fmt.Fprintf(&sb, "Found %d results:\n\n", len(results))

	for i, r := range results {
		fullURL := learnBaseURL + r.URL
		snippet := r.Snippet
		if len(snippet) > 200 {
			snippet = snippet[:200] + "…"
		}
		// Clean up snippet whitespace
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		snippet = strings.Join(strings.Fields(snippet), " ")

		fmt.Fprintf(&sb, "%d. %s\n", i+1, r.PageTitle)
		fmt.Fprintf(&sb, "   📖 %s > %s\n", r.CourseTitle, r.SectionTitle)
		fmt.Fprintf(&sb, "   🔗 %s\n", fullURL)
		fmt.Fprintf(&sb, "   %.0f%% match — %s\n\n", r.Score*100, snippet)
	}

	return sb.String()
}

// formatLearnResultsHTML builds an HTML version for Telegram HTML parse mode.
func formatLearnResultsHTML(query string, results []learnResult) string {
	if len(results) == 0 {
		return fmt.Sprintf("📚 No results found for <b>%s</b> on learn.biswas.me.", query)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "📚 <b>learn.biswas.me</b> — \"%s\"\n", query)
	fmt.Fprintf(&sb, "Found %d results:\n", len(results))

	for i, r := range results {
		fullURL := learnBaseURL + r.URL
		snippet := r.Snippet
		if len(snippet) > 180 {
			snippet = snippet[:180] + "…"
		}
		snippet = strings.ReplaceAll(snippet, "\n", " ")
		snippet = strings.Join(strings.Fields(snippet), " ")

		fmt.Fprintf(&sb, "\n<b>%d.</b> <a href=\"%s\">%s</a>\n", i+1, fullURL, r.PageTitle)
		fmt.Fprintf(&sb, "   📖 <i>%s</i> › %s\n", r.CourseTitle, r.SectionTitle)
		fmt.Fprintf(&sb, "   %.0f%% match · %s", r.Score*100, snippet)
	}

	return sb.String()
}
