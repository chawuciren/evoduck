package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
	"golang.org/x/net/html"
)

type WebSearchTool struct {
	client  *http.Client
	decider *proxy.Decider
}

func NewWebSearchTool(decider *proxy.Decider) *WebSearchTool {
	client := &http.Client{Timeout: 15 * time.Second}
	if decider != nil {
		client = decider.ForTool("web_search", "").HTTPClient
	}
	return &WebSearchTool{
		client:  client,
		decider: decider,
	}
}

func (t *WebSearchTool) Name() string {
	return "web_search"
}

func (t *WebSearchTool) Description() string {
	return `Search the web using multiple engines (Bing, DuckDuckGo, Brave, Startpage).

**When to use:**
- You need to find information or answers to questions on the web
- You need current news or recent information
- You want to find specific websites, articles, or documentation

**When NOT to use:**
- You already have a specific URL (use web_fetch)
- You're looking up library/API documentation (use context7 tools)
- You need to read a GitHub README (use web_fetch_github)

**Available search engines:**
- bing: Microsoft Bing search
- duckduckgo: DuckDuckGo search (privacy-focused)
- brave: Brave search (privacy-focused)
- startpage: Startpage search (privacy-focused, Google results)

Returns up to the specified number of results with title, URL, and description.`
}

func (t *WebSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query string",
			},
			"limit": map[string]interface{}{
				"type":        "number",
				"description": "Maximum number of results to return (default: 10, max: 50)",
			},
			"engines": map[string]interface{}{
				"type":        "array",
				"description": "Search engines to use (default: [\"bing\", \"duckduckgo\"]). Available: bing, duckduckgo, brave, startpage",
				"items": map[string]interface{}{
					"type": "string",
				},
			},
		},
		"required": []string{"query"},
	}
}

type searchResult struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Engine      string `json:"engine"`
}

// ExecuteWithRole 带角色检查的执行方法
func (t *WebSearchTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	return t.ExecuteWithContext(ctx, args)
}

func (t *WebSearchTool) Execute(args map[string]interface{}) (string, error) {
	return t.ExecuteWithContext(context.Background(), args)
}

// ExecuteWithContext 带上下文的执行，支持取消传播
func (t *WebSearchTool) ExecuteWithContext(ctx context.Context, args map[string]interface{}) (string, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("query is required")
	}

	limit := 10
	if l, ok := args["limit"].(float64); ok {
		limit = int(l)
		if limit < 1 {
			limit = 1
		}
		if limit > 50 {
			limit = 50
		}
	}

	engines := []string{"bing", "duckduckgo"}
	if e, ok := args["engines"].([]interface{}); ok && len(e) > 0 {
		engines = make([]string, 0, len(e))
		for _, eng := range e {
			if s, ok := eng.(string); ok {
				engines = append(engines, s)
			}
		}
	}

	if len(engines) == 0 {
		engines = []string{"bing", "duckduckgo"}
	}

	var allResults []searchResult
	var mu sync.Mutex
	var wg sync.WaitGroup

	perEngine := limit/len(engines) + 1
	if perEngine < 3 {
		perEngine = 3
	}

	for _, engine := range engines {
		wg.Add(1)
		go func(eng string) {
			defer wg.Done()
			results, err := t.searchWithEngine(ctx, eng, query, perEngine)
			if err != nil {
				return
			}
			mu.Lock()
			allResults = append(allResults, results...)
			mu.Unlock()
		}(engine)
	}
	wg.Wait()

	if len(allResults) == 0 {
		return fmt.Sprintf("No search results found for query: %s", query), nil
	}

	if len(allResults) > limit {
		allResults = allResults[:limit]
	}

	return formatSearchResults(allResults), nil
}

func (t *WebSearchTool) searchWithEngine(ctx context.Context, engine, query string, limit int) ([]searchResult, error) {
	switch engine {
	case "bing":
		return t.searchBing(ctx, query, limit)
	case "duckduckgo":
		return t.searchDuckDuckGo(ctx, query, limit)
	case "brave":
		return t.searchBrave(ctx, query, limit)
	case "startpage":
		return t.searchStartpage(ctx, query, limit)
	default:
		return nil, fmt.Errorf("unknown engine: %s", engine)
	}
}

func (t *WebSearchTool) searchBing(ctx context.Context, query string, limit int) ([]searchResult, error) {
	searchURL := fmt.Sprintf("https://cn.bing.com/search?q=%s&setlang=zh-CN&ensearch=0&first=0",
		url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,zh-CN;q=0.8,zh;q=0.7")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("bing returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return parseBingResults(string(body), limit), nil
}

func parseBingResults(htmlContent string, limit int) []searchResult {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil
	}

	var results []searchResult
	var currentResult *searchResult
	var inResult, inTitle, inDesc bool
	var titleHref string

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "li" {
				for _, attr := range n.Attr {
					if attr.Key == "class" && strings.Contains(attr.Val, "b_algo") {
						currentResult = &searchResult{Engine: "bing"}
						inResult = true
						break
					}
				}
			}
			if inResult {
				if n.Data == "h2" || n.Data == "a" {
					for _, attr := range n.Attr {
						if attr.Key == "href" && currentResult != nil && currentResult.URL == "" {
							titleHref = attr.Val
						}
					}
					inTitle = true
				}
				if n.Data == "p" || (n.Data == "div" && hasClass(n, "b_caption")) {
					inDesc = true
				}
			}
		}

		if n.Type == html.TextNode && inResult && currentResult != nil {
			text := strings.TrimSpace(n.Data)
			if text == "" {
				goto next
			}
			if inTitle && currentResult.Title == "" {
				currentResult.Title = text
				if titleHref != "" {
					currentResult.URL = titleHref
					titleHref = ""
				}
			} else if inDesc {
				if currentResult.Description != "" {
					currentResult.Description += " "
				}
				currentResult.Description += text
			}
		}

	next:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}

		if n.Type == html.ElementNode && inResult {
			if n.Data == "h2" || n.Data == "a" {
				inTitle = false
			}
			if n.Data == "p" || n.Data == "div" {
				inDesc = false
			}
			if n.Data == "li" {
				if currentResult != nil && currentResult.URL != "" && currentResult.Title != "" {
					results = append(results, *currentResult)
					if len(results) >= limit {
						return
					}
				}
				currentResult = nil
				inResult = false
			}
		}
	}

	walk(doc)

	return results
}

func (t *WebSearchTool) searchDuckDuckGo(ctx context.Context, query string, limit int) ([]searchResult, error) {
	formData := url.Values{}
	formData.Set("q", query)

	req, err := http.NewRequestWithContext(ctx, "POST", "https://html.duckduckgo.com/html/",
		strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("duckduckgo returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return parseDuckDuckGoResults(string(body), limit), nil
}

func parseDuckDuckGoResults(htmlContent string, limit int) []searchResult {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil
	}

	var results []searchResult
	var currentResult *searchResult
	var inResult, inLink, inSnippet bool

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "div" && hasClass(n, "result") {
				currentResult = &searchResult{Engine: "duckduckgo"}
				inResult = true
			}
			if inResult && n.Data == "a" && hasClass(n, "result__a") {
				inLink = true
				for _, attr := range n.Attr {
					if attr.Key == "href" {
						currentResult.URL = attr.Val
					}
				}
			}
			if inResult && n.Data == "a" && hasClass(n, "result__snippet") {
				inSnippet = true
			}
		}

		if n.Type == html.TextNode && inResult && currentResult != nil {
			text := strings.TrimSpace(n.Data)
			if text == "" {
				goto next
			}
			if inLink && currentResult.Title == "" {
				currentResult.Title = text
			}
			if inSnippet {
				if currentResult.Description != "" {
					currentResult.Description += " "
				}
				currentResult.Description += text
			}
		}

	next:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}

		if n.Type == html.ElementNode && inResult {
			if n.Data == "a" {
				inLink = false
				inSnippet = false
			}
			if n.Data == "div" && hasClass(n, "result") {
				if currentResult != nil && currentResult.URL != "" {
					results = append(results, *currentResult)
					if len(results) >= limit {
						return
					}
				}
				currentResult = nil
				inResult = false
			}
		}
	}

	walk(doc)

	return results
}

func (t *WebSearchTool) searchBrave(ctx context.Context, query string, limit int) ([]searchResult, error) {
	searchURL := fmt.Sprintf("https://search.brave.com/search?q=%s&source=web&offset=0",
		url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Referer", "https://duckduckgo.com/")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brave returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return parseBraveResults(string(body), limit), nil
}

func parseBraveResults(htmlContent string, limit int) []searchResult {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil
	}

	var results []searchResult
	var currentResult *searchResult
	var inSnippet, inTitle bool
	var saveNextText bool

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "div" && hasClass(n, "snippet") {
				currentResult = &searchResult{Engine: "brave"}
				inSnippet = true
			}
			if inSnippet && n.Data == "a" && hasClass(n, "snippet-title") {
				inTitle = true
				for _, attr := range n.Attr {
					if attr.Key == "href" {
						currentResult.URL = attr.Val
					}
				}
			}
			if inSnippet && n.Data == "div" && hasClass(n, "snippet-description") {
				saveNextText = true
			}
		}

		if n.Type == html.TextNode && currentResult != nil {
			text := strings.TrimSpace(n.Data)
			if text == "" {
				goto next
			}
			if inTitle && currentResult.Title == "" {
				currentResult.Title = text
			}
			if saveNextText {
				if currentResult.Description != "" {
					currentResult.Description += " "
				}
				currentResult.Description += text
			}
		}

	next:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}

		if n.Type == html.ElementNode && inSnippet {
			if n.Data == "a" {
				inTitle = false
			}
			if n.Data == "div" && hasClass(n, "snippet-description") {
				saveNextText = false
			}
			if n.Data == "div" && hasClass(n, "snippet") {
				if currentResult != nil && currentResult.URL != "" {
					results = append(results, *currentResult)
					if len(results) >= limit {
						return
					}
				}
				currentResult = nil
				inSnippet = false
			}
		}
	}

	walk(doc)

	return results
}

func (t *WebSearchTool) searchStartpage(ctx context.Context, query string, limit int) ([]searchResult, error) {
	scToken, err := t.getStartpageCSToken(ctx)
	if err != nil {
		return nil, err
	}

	formData := url.Values{}
	formData.Set("query", query)
	formData.Set("cat", "web")
	formData.Set("sc", scToken)
	formData.Set("page", "1")

	req, err := http.NewRequestWithContext(ctx, "POST", "https://www.startpage.com/sp/search",
		strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("startpage returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return parseStartpageResults(string(body), limit), nil
}

var startpageCSTokenCache struct {
	token     string
	expiresAt time.Time
	mu        sync.Mutex
}

func (t *WebSearchTool) getStartpageCSToken(ctx context.Context) (string, error) {
	startpageCSTokenCache.mu.Lock()
	defer startpageCSTokenCache.mu.Unlock()

	if startpageCSTokenCache.token != "" && time.Now().Before(startpageCSTokenCache.expiresAt) {
		return startpageCSTokenCache.token, nil
	}

	req, err := http.NewRequestWithContext(ctx, "GET", "https://www.startpage.com/", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	token := extractStartpageCSToken(string(body))
	if token == "" {
		return "", fmt.Errorf("could not find startpage CSRF token")
	}

	startpageCSTokenCache.token = token
	startpageCSTokenCache.expiresAt = time.Now().Add(25 * time.Minute)

	return token, nil
}

func extractStartpageCSToken(html string) string {
	search := `<input type="hidden" name="sc" value="`
	idx := strings.Index(html, search)
	if idx < 0 {
		return ""
	}
	start := idx + len(search)
	end := strings.Index(html[start:], `"`)
	if end < 0 {
		return ""
	}
	return html[start : start+end]
}

func parseStartpageResults(htmlContent string, limit int) []searchResult {
	doc, err := html.Parse(strings.NewReader(htmlContent))
	if err != nil {
		return nil
	}

	var results []searchResult
	var currentResult *searchResult
	var inResult, inLink, inDesc bool

	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			if n.Data == "div" && hasClass(n, "result") {
				currentResult = &searchResult{Engine: "startpage"}
				inResult = true
			}
			if inResult && n.Data == "a" && hasClass(n, "result-title") {
				inLink = true
				for _, attr := range n.Attr {
					if attr.Key == "href" {
						currentResult.URL = attr.Val
					}
				}
			}
			if inResult && n.Data == "p" && hasClass(n, "description") {
				inDesc = true
			}
		}

		if n.Type == html.TextNode && inResult && currentResult != nil {
			text := strings.TrimSpace(n.Data)
			if text == "" {
				goto next
			}
			if inLink && currentResult.Title == "" {
				currentResult.Title = text
			}
			if inDesc {
				if currentResult.Description != "" {
					currentResult.Description += " "
				}
				currentResult.Description += text
			}
		}

	next:
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}

		if n.Type == html.ElementNode && inResult {
			if n.Data == "a" {
				inLink = false
			}
			if n.Data == "p" {
				inDesc = false
			}
			if n.Data == "div" && hasClass(n, "result") {
				if currentResult != nil && currentResult.URL != "" {
					results = append(results, *currentResult)
					if len(results) >= limit {
						return
					}
				}
				currentResult = nil
				inResult = false
			}
		}
	}

	walk(doc)

	return results
}

func hasClass(n *html.Node, class string) bool {
	for _, attr := range n.Attr {
		if attr.Key == "class" {
			for _, c := range strings.Fields(attr.Val) {
				if c == class {
					return true
				}
			}
		}
	}
	return false
}

func formatSearchResults(results []searchResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Web search results (%d results):\n\n", len(results)))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, r.Title))
		sb.WriteString(fmt.Sprintf("   URL: %s\n", r.URL))
		if r.Description != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", r.Description))
		}
		sb.WriteString(fmt.Sprintf("   Source: %s\n\n", r.Engine))
	}
	return sb.String()
}
