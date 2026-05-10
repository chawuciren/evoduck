package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chawuciren/evoduck/pkg/proxy"
)

type WebFetchTool struct {
	client  *http.Client
	decider *proxy.Decider
}

func NewWebFetchTool(decider *proxy.Decider) *WebFetchTool {
	client := &http.Client{Timeout: 30 * time.Second}
	if decider != nil {
		client = decider.ForTool("web_fetch", "").HTTPClient
	}
	return &WebFetchTool{
		client:  client,
		decider: decider,
	}
}

func (t *WebFetchTool) Name() string {
	return "web_fetch"
}

func (t *WebFetchTool) Description() string {
	return `Fetch and read full content from a specific URL. Supports both HTML pages and Markdown files.

**When to use:**
- You have a specific URL and need the full content
- After web_search, to get details from a result URL
- Reading articles, documentation, or web pages

**When NOT to use:**
- You don't have a URL yet (use web_search first)
- You need a GitHub README (use web_fetch_github for better reliability)
- You just need to find information (use web_search)

**Examples:**
- web_search found "https://example.com/article" → web_fetch(url="https://example.com/article")
- User gives you a URL → web_fetch(url="https://...")

Returns up to maxChars characters of cleaned content (default 30000).`
}

func (t *WebFetchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to fetch content from (public HTTP(S) only)",
			},
			"maxChars": map[string]interface{}{
				"type":        "number",
				"description": "Maximum characters to return (default: 30000, range: 1000-200000)",
			},
			"readability": map[string]interface{}{
				"type":        "boolean",
				"description": "Use readability extraction to remove navigation, ads, and sidebars (default: true)",
			},
		},
		"required": []string{"url"},
	}
}

// ExecuteWithRole 带角色检查的执行方法
func (t *WebFetchTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	return t.ExecuteWithContext(ctx, args)
}

func (t *WebFetchTool) Execute(args map[string]interface{}) (string, error) {
	return t.ExecuteWithContext(context.Background(), args)
}

// ExecuteWithContext 带上下文的执行，支持取消传播
func (t *WebFetchTool) ExecuteWithContext(ctx context.Context, args map[string]interface{}) (string, error) {
	rawURL, ok := args["url"].(string)
	if !ok || rawURL == "" {
		return "", fmt.Errorf("url is required")
	}

	if _, err := url.ParseRequestURI(rawURL); err != nil {
		return "", fmt.Errorf("invalid URL: %w", err)
	}

	maxChars := 30000
	if mc, ok := args["maxChars"].(float64); ok {
		maxChars = int(mc)
		if maxChars < 1000 {
			maxChars = 1000
		}
		if maxChars > 200000 {
			maxChars = 200000
		}
	}

	// 使用 decider 为目标 URL 决定代理
	client := t.client
	if t.decider != nil {
		decision := t.decider.ForTool("web_fetch", rawURL)
		client = decision.HTTPClient
	}

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; OpencodeBot/1.0)")
	req.Header.Set("Accept", "text/html,text/plain,text/markdown,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(maxChars*2)))
	if err != nil {
		return "", fmt.Errorf("read body: %w", err)
	}

	content := string(body)
	contentType := resp.Header.Get("Content-Type")

	isHTML := strings.Contains(contentType, "text/html") || strings.Contains(contentType, "application/xhtml")
	isMarkdown := strings.Contains(contentType, "text/markdown") || strings.HasSuffix(rawURL, ".md")

	if isMarkdown || !isHTML {
		result := content
		if len(result) > maxChars {
			result = result[:maxChars] + fmt.Sprintf("\n\n...(truncated %d remaining characters)", len(result)-maxChars)
		}
		return result, nil
	}

	readability := true
	if r, ok := args["readability"].(bool); ok {
		readability = r
	}

	cleaned := cleanHTMLContent(content, readability)

	if len(cleaned) > maxChars {
		cleaned = cleaned[:maxChars] + fmt.Sprintf("\n\n...(truncated %d remaining characters)", len(cleaned)-maxChars)
	}

	if strings.TrimSpace(cleaned) == "" {
		return "(page returned empty or non-text content)", nil
	}

	return cleaned, nil
}

func cleanHTMLContent(htmlStr string, readability bool) string {
	if readability {
		htmlStr = removeElements(htmlStr, []string{"script", "style", "nav", "footer", "header", "noscript"})
		htmlStr = removeClassElements(htmlStr, []string{"sidebar", "comment", "advertisement", "nav", "footer", "header", "menu"})
	}

	htmlStr = strings.ReplaceAll(htmlStr, "<br>", "\n")
	htmlStr = strings.ReplaceAll(htmlStr, "<br/>", "\n")
	htmlStr = strings.ReplaceAll(htmlStr, "<br />", "\n")
	htmlStr = strings.ReplaceAll(htmlStr, "</p>", "\n\n")
	htmlStr = strings.ReplaceAll(htmlStr, "</div>", "\n")
	htmlStr = strings.ReplaceAll(htmlStr, "</li>", "\n")
	htmlStr = strings.ReplaceAll(htmlStr, "</h1>", "\n\n")
	htmlStr = strings.ReplaceAll(htmlStr, "</h2>", "\n\n")
	htmlStr = strings.ReplaceAll(htmlStr, "</h3>", "\n")
	htmlStr = strings.ReplaceAll(htmlStr, "</h4>", "\n")
	htmlStr = strings.ReplaceAll(htmlStr, "</tr>", "\n")
	htmlStr = strings.ReplaceAll(htmlStr, "<hr>", "\n---\n")
	htmlStr = strings.ReplaceAll(htmlStr, "<hr/>", "\n---\n")

	htmlStr = stripTags(htmlStr)

	lines := strings.Split(htmlStr, "\n")
	var cleaned []string
	var prevBlank bool
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			if !prevBlank {
				cleaned = append(cleaned, "")
				prevBlank = true
			}
		} else {
			cleaned = append(cleaned, trimmed)
			prevBlank = false
		}
	}

	return strings.TrimSpace(strings.Join(cleaned, "\n"))
}

func removeElements(htmlStr string, tags []string) string {
	for _, tag := range tags {
		var result strings.Builder
		result.Grow(len(htmlStr))
		remaining := htmlStr
		for {
			openTag := "<" + tag
			closeTag := "</" + tag + ">"

			idx := indexASCIIInsensitive(remaining, openTag)
			if idx < 0 {
				result.WriteString(remaining)
				break
			}

			result.WriteString(remaining[:idx])
			remaining = remaining[idx:]

			endIdx := indexASCIIInsensitive(remaining, closeTag)
			if endIdx < 0 {
				endIdx = strings.Index(remaining[1:], ">")
				if endIdx < 0 {
					result.WriteString(remaining)
					break
				}
				remaining = remaining[endIdx+2:]
				continue
			}

			next := endIdx + len(closeTag)
			if next > len(remaining) {
				result.WriteString(remaining)
				break
			}
			remaining = remaining[next:]
		}
		htmlStr = result.String()
	}
	return htmlStr
}

func removeClassElements(htmlStr string, classes []string) string {
	for _, class := range classes {
		classAttrs := []string{
			`class="` + class + `"`,
			`class='` + class + `'`,
		}

		for {
			idx := -1
			for _, classAttr := range classAttrs {
				found := indexASCIIInsensitive(htmlStr, classAttr)
				if found >= 0 && (idx < 0 || found < idx) {
					idx = found
				}
			}
			if idx < 0 {
				break
			}

			start := idx
			for start > 0 && htmlStr[start-1] != '<' {
				start--
			}
			if start == 0 {
				break
			}
			start--

			tagStart := htmlStr[start:]
			spaceIdx := strings.Index(tagStart, " ")
			closeIdx := strings.Index(tagStart, ">")
			var tagName string
			if spaceIdx > 0 && (closeIdx < 0 || spaceIdx < closeIdx) {
				tagName = tagStart[1:spaceIdx]
			} else if closeIdx > 0 {
				tagName = tagStart[1:closeIdx]
			}

			closeTag := "</" + tagName + ">"
			endIdx := indexASCIIInsensitive(htmlStr[idx:], closeTag)
			if endIdx < 0 {
				htmlStr = htmlStr[:start]
				break
			}

			next := idx + endIdx + len(closeTag)
			if next > len(htmlStr) {
				htmlStr = htmlStr[:start]
				break
			}
			htmlStr = htmlStr[:start] + htmlStr[next:]
		}
	}
	return htmlStr
}

func indexASCIIInsensitive(s, substr string) int {
	if len(substr) == 0 {
		return 0
	}
	if len(substr) > len(s) {
		return -1
	}

	first := lowerASCII(substr[0])
	limit := len(s) - len(substr)
	for i := 0; i <= limit; i++ {
		if lowerASCII(s[i]) != first {
			continue
		}
		if hasASCIIInsensitivePrefix(s[i:], substr) {
			return i
		}
	}

	return -1
}

func hasASCIIInsensitivePrefix(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	for i := 0; i < len(prefix); i++ {
		if lowerASCII(s[i]) != lowerASCII(prefix[i]) {
			return false
		}
	}
	return true
}

func lowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

func stripTags(htmlStr string) string {
	var result strings.Builder
	inTag := false
	for _, r := range htmlStr {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			result.WriteRune(r)
		}
	}
	return result.String()
}
