package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chawuciren/evoduck/pkg/models"
	"github.com/chromedp/chromedp"
)

const (
	defaultBrowserActionTimeout   = 15 * time.Second
	defaultBrowserNavigateTimeout = 30 * time.Second
)

func browserActionContext(parent context.Context, browserCtx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if parent == nil {
		parent = context.Background()
	}
	if timeout <= 0 {
		timeout = defaultBrowserActionTimeout
	}

	ctx, cancel := context.WithTimeout(browserCtx, timeout)
	done := make(chan struct{})
	go func() {
		select {
		case <-parent.Done():
			cancel()
		case <-done:
		}
	}()

	return ctx, func() {
		close(done)
		cancel()
	}
}

func browserActionError(action string, timeout time.Duration, err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%s timed out after %s", action, timeout)
	}
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("%s canceled", action)
	}
	return err
}

type BrowserNavigateTool struct {
	manager *BrowserManager
}

func NewBrowserNavigateTool(manager *BrowserManager) *BrowserNavigateTool {
	return &BrowserNavigateTool{manager: manager}
}

func (t *BrowserNavigateTool) Name() string { return "browser_navigate" }

func (t *BrowserNavigateTool) Description() string {
	return `Navigate to a URL in the browser.

**When to use:**
- Opening a specific webpage
- Starting a browsing session
- Navigating to a new page

**Parameters:**
- url: The URL to navigate to (required)

Returns confirmation of navigation.`
}

func (t *BrowserNavigateTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"url": map[string]interface{}{
				"type":        "string",
				"description": "The URL to navigate to",
			},
		},
		"required": []string{"url"},
	}
}

func (t *BrowserNavigateTool) Execute(args map[string]interface{}) (string, error) {
	return t.execute(context.Background(), args)
}

func (t *BrowserNavigateTool) execute(parent context.Context, args map[string]interface{}) (string, error) {
	url, ok := args["url"].(string)
	if !ok || url == "" {
		return "", fmt.Errorf("url is required")
	}

	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}

	browserCtx, err := t.manager.GetOrCreateBrowser()
	if err != nil {
		return "", err
	}
	ctx, cancel := browserActionContext(parent, browserCtx, defaultBrowserNavigateTimeout)
	defer cancel()

	if err := chromedp.Run(ctx, chromedp.Navigate(url)); err != nil {
		return "", browserActionError("navigate", defaultBrowserNavigateTimeout, err)
	}
	t.manager.SetCurrentURL(url)

	return fmt.Sprintf("Navigated to: %s", url), nil
}

type BrowserClickTool struct {
	manager *BrowserManager
}

func NewBrowserClickTool(manager *BrowserManager) *BrowserClickTool {
	return &BrowserClickTool{manager: manager}
}

func (t *BrowserClickTool) Name() string { return "browser_click" }

func (t *BrowserClickTool) Description() string {
	return `Click an element on the page.

**When to use:**
- Clicking buttons, links, or other clickable elements
- Submitting forms
- Interacting with UI components

**Parameters:**
- selector: CSS selector for the element (required)
- element: Human-readable description of the element (optional)

Returns confirmation of click.`
}

func (t *BrowserClickTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector for the element to click",
			},
			"element": map[string]interface{}{
				"type":        "string",
				"description": "Human-readable description of the element",
			},
		},
		"required": []string{"selector"},
	}
}

func (t *BrowserClickTool) Execute(args map[string]interface{}) (string, error) {
	return t.execute(context.Background(), args)
}

func (t *BrowserClickTool) execute(parent context.Context, args map[string]interface{}) (string, error) {
	selector, ok := args["selector"].(string)
	if !ok || selector == "" {
		return "", fmt.Errorf("selector is required")
	}

	browserCtx, err := t.manager.GetOrCreateBrowser()
	if err != nil {
		return "", err
	}
	ctx, cancel := browserActionContext(parent, browserCtx, defaultBrowserActionTimeout)
	defer cancel()

	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.Click(selector, chromedp.ByQuery),
	); err != nil {
		return "", browserActionError("click element", defaultBrowserActionTimeout, err)
	}

	return fmt.Sprintf("Clicked element: %s", selector), nil
}

type BrowserTypeTool struct {
	manager *BrowserManager
}

func NewBrowserTypeTool(manager *BrowserManager) *BrowserTypeTool {
	return &BrowserTypeTool{manager: manager}
}

func (t *BrowserTypeTool) Name() string { return "browser_type" }

func (t *BrowserTypeTool) Description() string {
	return `Type text into an input field or textarea.

**When to use:**
- Filling form fields
- Entering search queries
- Typing into text inputs

**Parameters:**
- selector: CSS selector for the input element (required)
- text: Text to type (required)
- submit: Press Enter after typing (optional, default false)

Returns confirmation of typing.`
}

func (t *BrowserTypeTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector for the input element",
			},
			"text": map[string]interface{}{
				"type":        "string",
				"description": "Text to type into the element",
			},
			"submit": map[string]interface{}{
				"type":        "boolean",
				"description": "Press Enter after typing (default: false)",
			},
		},
		"required": []string{"selector", "text"},
	}
}

func (t *BrowserTypeTool) Execute(args map[string]interface{}) (string, error) {
	return t.execute(context.Background(), args)
}

func (t *BrowserTypeTool) execute(parent context.Context, args map[string]interface{}) (string, error) {
	selector, ok := args["selector"].(string)
	if !ok || selector == "" {
		return "", fmt.Errorf("selector is required")
	}

	text, ok := args["text"].(string)
	if !ok {
		return "", fmt.Errorf("text is required")
	}

	browserCtx, err := t.manager.GetOrCreateBrowser()
	if err != nil {
		return "", err
	}
	ctx, cancel := browserActionContext(parent, browserCtx, defaultBrowserActionTimeout)
	defer cancel()

	actions := []chromedp.Action{
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.SendKeys(selector, text, chromedp.ByQuery),
	}

	submit, _ := args["submit"].(bool)
	if submit {
		actions = append(actions, chromedp.SendKeys(selector, "\n", chromedp.ByQuery))
	}

	if err := chromedp.Run(ctx, actions...); err != nil {
		return "", browserActionError("type into element", defaultBrowserActionTimeout, err)
	}

	return fmt.Sprintf("Typed '%s' into: %s", text, selector), nil
}

type browserScreenshotResult struct {
	Summary string                 `json:"summary"`
	Media   []models.OutgoingMedia `json:"media,omitempty"`
}

type BrowserScreenshotTool struct {
	manager *BrowserManager
}

func NewBrowserScreenshotTool(manager *BrowserManager) *BrowserScreenshotTool {
	return &BrowserScreenshotTool{manager: manager}
}

func (t *BrowserScreenshotTool) Name() string { return "browser_screenshot" }

func (t *BrowserScreenshotTool) Description() string {
	return `Take a screenshot of the current page.

**When to use:**
- Capturing page state for debugging
- Visual verification
- Documenting page appearance

**Parameters:**
- fullPage: Capture entire scrollable page (optional, default false)
- selector: Capture specific element only (optional)

Returns a screenshot result with summary text and PNG media.`
}

func (t *BrowserScreenshotTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"fullPage": map[string]interface{}{
				"type":        "boolean",
				"description": "Capture entire scrollable page (default: false)",
			},
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector to capture specific element",
			},
		},
	}
}

func (t *BrowserScreenshotTool) Execute(args map[string]interface{}) (string, error) {
	return t.execute(context.Background(), args)
}

func (t *BrowserScreenshotTool) execute(parent context.Context, args map[string]interface{}) (string, error) {
	browserCtx, err := t.manager.GetOrCreateBrowser()
	if err != nil {
		return "", err
	}
	ctx, cancel := browserActionContext(parent, browserCtx, defaultBrowserActionTimeout)
	defer cancel()

	var buf []byte
	fullPage, _ := args["fullPage"].(bool)
	selector, _ := args["selector"].(string)

	if selector != "" {
		if err := chromedp.Run(ctx, chromedp.Screenshot(selector, &buf, chromedp.ByQuery)); err != nil {
			return "", browserActionError("capture element screenshot", defaultBrowserActionTimeout, err)
		}
	} else if fullPage {
		if err := chromedp.Run(ctx, chromedp.FullScreenshot(&buf, 90)); err != nil {
			return "", browserActionError("capture full page screenshot", defaultBrowserActionTimeout, err)
		}
	} else {
		if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&buf)); err != nil {
			return "", browserActionError("capture screenshot", defaultBrowserActionTimeout, err)
		}
	}

	summary := fmt.Sprintf("Screenshot captured (%d bytes)", len(buf))
	name := "browser-screenshot.png"
	if selector != "" {
		summary = fmt.Sprintf("Element screenshot captured for %s (%d bytes)", selector, len(buf))
		name = "browser-element-screenshot.png"
	} else if fullPage {
		summary = fmt.Sprintf("Full-page screenshot captured (%d bytes)", len(buf))
		name = "browser-fullpage-screenshot.png"
	}

	payload, err := json.Marshal(browserScreenshotResult{
		Summary: summary,
		Media: []models.OutgoingMedia{{
			Type:     "image",
			Name:     name,
			MimeType: "image/png",
			Data:     base64.StdEncoding.EncodeToString(buf),
			FileSize: int64(len(buf)),
		}},
	})
	if err != nil {
		return "", fmt.Errorf("marshal screenshot result: %w", err)
	}

	return string(payload), nil
}

type BrowserSnapshotTool struct {
	manager *BrowserManager
}

func NewBrowserSnapshotTool(manager *BrowserManager) *BrowserSnapshotTool {
	return &BrowserSnapshotTool{manager: manager}
}

func (t *BrowserSnapshotTool) Name() string { return "browser_snapshot" }

func (t *BrowserSnapshotTool) Description() string {
	return `Get a text snapshot of the current page structure.

**When to use:**
- Understanding page layout and content
- Finding elements to interact with
- Debugging page state

**Parameters:**
- selector: CSS selector to get specific element content (optional, default: body)

Returns page text content with structure.`
}

func (t *BrowserSnapshotTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector for specific element (default: body)",
			},
		},
	}
}

func (t *BrowserSnapshotTool) Execute(args map[string]interface{}) (string, error) {
	return t.execute(context.Background(), args)
}

func (t *BrowserSnapshotTool) execute(parent context.Context, args map[string]interface{}) (string, error) {
	browserCtx, err := t.manager.GetOrCreateBrowser()
	if err != nil {
		return "", err
	}
	ctx, cancel := browserActionContext(parent, browserCtx, defaultBrowserActionTimeout)
	defer cancel()

	selector, _ := args["selector"].(string)
	if selector == "" {
		selector = "body"
	}

	var text string
	var html string
	var url string

	actions := []chromedp.Action{
		chromedp.Location(&url),
		chromedp.Text(selector, &text, chromedp.ByQuery),
		chromedp.OuterHTML(selector, &html, chromedp.ByQuery),
	}

	if err := chromedp.Run(ctx, actions...); err != nil {
		return "", browserActionError("capture page snapshot", defaultBrowserActionTimeout, err)
	}

	if len(text) > 5000 {
		text = text[:5000] + "...(truncated)"
	}

	result := fmt.Sprintf("URL: %s\n\nContent:\n%s", url, text)
	return result, nil
}

type BrowserCloseTool struct {
	manager *BrowserManager
}

func NewBrowserCloseTool(manager *BrowserManager) *BrowserCloseTool {
	return &BrowserCloseTool{manager: manager}
}

func (t *BrowserCloseTool) Name() string { return "browser_close" }

func (t *BrowserCloseTool) Description() string {
	return `Close the browser and release resources.

**When to use:**
- Ending a browsing session
- Cleaning up after automation
- Freeing browser resources

No parameters required.

Returns confirmation of closure.`
}

func (t *BrowserCloseTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}

func (t *BrowserCloseTool) Execute(args map[string]interface{}) (string, error) {
	t.manager.Close()
	return "Browser closed", nil
}

type BrowserEvaluateTool struct {
	manager *BrowserManager
}

func NewBrowserEvaluateTool(manager *BrowserManager) *BrowserEvaluateTool {
	return &BrowserEvaluateTool{manager: manager}
}

func (t *BrowserEvaluateTool) Name() string { return "browser_evaluate" }

func (t *BrowserEvaluateTool) Description() string {
	return `Execute JavaScript in the browser.

**When to use:**
- Running custom JavaScript
- Extracting data not available via DOM
- Modifying page state

**Parameters:**
- script: JavaScript code to execute (required)

Returns the result of the evaluation.`
}

func (t *BrowserEvaluateTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"script": map[string]interface{}{
				"type":        "string",
				"description": "JavaScript code to execute",
			},
		},
		"required": []string{"script"},
	}
}

func (t *BrowserEvaluateTool) Execute(args map[string]interface{}) (string, error) {
	return t.execute(context.Background(), args)
}

func (t *BrowserEvaluateTool) execute(parent context.Context, args map[string]interface{}) (string, error) {
	script, ok := args["script"].(string)
	if !ok || script == "" {
		return "", fmt.Errorf("script is required")
	}

	browserCtx, err := t.manager.GetOrCreateBrowser()
	if err != nil {
		return "", err
	}
	ctx, cancel := browserActionContext(parent, browserCtx, defaultBrowserActionTimeout)
	defer cancel()

	var result interface{}
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, &result)); err != nil {
		return "", browserActionError("evaluate script", defaultBrowserActionTimeout, err)
	}

	return fmt.Sprintf("Result: %v", result), nil
}

type BrowserWaitForTool struct {
	manager *BrowserManager
}

func NewBrowserWaitForTool(manager *BrowserManager) *BrowserWaitForTool {
	return &BrowserWaitForTool{manager: manager}
}

func (t *BrowserWaitForTool) Name() string { return "browser_wait_for" }

func (t *BrowserWaitForTool) Description() string {
	return `Wait for an element or text to appear on the page.

**When to use:**
- Waiting for page to load
- Waiting for dynamic content
- Waiting for specific elements

**Parameters:**
- selector: CSS selector to wait for (optional)
- text: Text to wait for (optional)
- timeout: Timeout in seconds (optional, default: 10)

Returns confirmation when condition is met.`
}

func (t *BrowserWaitForTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector to wait for visible",
			},
			"text": map[string]interface{}{
				"type":        "string",
				"description": "Text to wait for on page",
			},
			"timeout": map[string]interface{}{
				"type":        "number",
				"description": "Timeout in seconds (default: 10)",
			},
		},
	}
}

func (t *BrowserWaitForTool) Execute(args map[string]interface{}) (string, error) {
	return t.execute(context.Background(), args)
}

func (t *BrowserWaitForTool) execute(parent context.Context, args map[string]interface{}) (string, error) {
	timeout := 10 * time.Second
	if t, ok := args["timeout"].(float64); ok && t > 0 {
		timeout = time.Duration(t) * time.Second
	}

	selector, _ := args["selector"].(string)
	text, _ := args["text"].(string)

	if selector == "" && text == "" {
		return "", fmt.Errorf("either selector or text is required")
	}

	browserCtx, err := t.manager.GetOrCreateBrowser()
	if err != nil {
		return "", err
	}

	timeoutCtx, cancel := browserActionContext(parent, browserCtx, timeout)
	defer cancel()

	if selector != "" {
		if err := chromedp.Run(timeoutCtx, chromedp.WaitVisible(selector, chromedp.ByQuery)); err != nil {
			return "", browserActionError("wait for selector", timeout, err)
		}
		return fmt.Sprintf("Element visible: %s", selector), nil
	}

	script := fmt.Sprintf(`document.body.innerText.includes('%s')`, text)
	var found bool
	if err := chromedp.Run(timeoutCtx, chromedp.Evaluate(script, &found)); err != nil {
		return "", browserActionError("wait for text", timeout, err)
	}
	if !found {
		return "", fmt.Errorf("text not found within timeout: %s", text)
	}
	return fmt.Sprintf("Text found: %s", text), nil
}

type BrowserScrollTool struct {
	manager *BrowserManager
}

func NewBrowserScrollTool(manager *BrowserManager) *BrowserScrollTool {
	return &BrowserScrollTool{manager: manager}
}

func (t *BrowserScrollTool) Name() string { return "browser_scroll" }

func (t *BrowserScrollTool) Description() string {
	return `Scroll the page or scroll an element into view.

**When to use:**
- Scrolling to find more content
- Bringing elements into view
- Navigating long pages

**Parameters:**
- selector: CSS selector to scroll into view (optional)
- direction: Scroll direction - "up" or "down" (optional, default: down)

Returns confirmation of scroll.`
}

func (t *BrowserScrollTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector to scroll into view",
			},
			"direction": map[string]interface{}{
				"type":        "string",
				"description": "Scroll direction: up or down (default: down)",
			},
		},
	}
}

func (t *BrowserScrollTool) Execute(args map[string]interface{}) (string, error) {
	return t.execute(context.Background(), args)
}

func (t *BrowserScrollTool) execute(parent context.Context, args map[string]interface{}) (string, error) {
	browserCtx, err := t.manager.GetOrCreateBrowser()
	if err != nil {
		return "", err
	}
	ctx, cancel := browserActionContext(parent, browserCtx, defaultBrowserActionTimeout)
	defer cancel()

	selector, _ := args["selector"].(string)
	direction, _ := args["direction"].(string)
	if direction == "" {
		direction = "down"
	}

	if selector != "" {
		if err := chromedp.Run(ctx, chromedp.ScrollIntoView(selector, chromedp.ByQuery)); err != nil {
			return "", browserActionError("scroll element into view", defaultBrowserActionTimeout, err)
		}
		return fmt.Sprintf("Scrolled element into view: %s", selector), nil
	}

	deltaY := 500
	if direction == "up" {
		deltaY = -500
	}

	script := fmt.Sprintf("window.scrollBy(0, %d)", deltaY)
	if err := chromedp.Run(ctx, chromedp.Evaluate(script, nil)); err != nil {
		return "", browserActionError("scroll page", defaultBrowserActionTimeout, err)
	}

	return fmt.Sprintf("Scrolled %s", direction), nil
}

type BrowserGetHTMLTool struct {
	manager *BrowserManager
}

func NewBrowserGetHTMLTool(manager *BrowserManager) *BrowserGetHTMLTool {
	return &BrowserGetHTMLTool{manager: manager}
}

func (t *BrowserGetHTMLTool) Name() string { return "browser_get_html" }

func (t *BrowserGetHTMLTool) Description() string {
	return `Get the HTML content of an element or the entire page.

**When to use:**
- Extracting page structure
- Getting element markup
- Debugging page content

**Parameters:**
- selector: CSS selector for element (optional, default: body)

Returns HTML content.`
}

func (t *BrowserGetHTMLTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector for element (default: body)",
			},
		},
	}
}

func (t *BrowserGetHTMLTool) Execute(args map[string]interface{}) (string, error) {
	return t.execute(context.Background(), args)
}

func (t *BrowserGetHTMLTool) execute(parent context.Context, args map[string]interface{}) (string, error) {
	browserCtx, err := t.manager.GetOrCreateBrowser()
	if err != nil {
		return "", err
	}
	ctx, cancel := browserActionContext(parent, browserCtx, defaultBrowserActionTimeout)
	defer cancel()

	selector, _ := args["selector"].(string)
	if selector == "" {
		selector = "body"
	}

	var html string
	if err := chromedp.Run(ctx, chromedp.OuterHTML(selector, &html, chromedp.ByQuery)); err != nil {
		return "", browserActionError("get html", defaultBrowserActionTimeout, err)
	}

	if len(html) > 30000 {
		html = html[:30000] + "...(truncated)"
	}

	return html, nil
}

type BrowserHoverTool struct {
	manager *BrowserManager
}

func NewBrowserHoverTool(manager *BrowserManager) *BrowserHoverTool {
	return &BrowserHoverTool{manager: manager}
}

func (t *BrowserHoverTool) Name() string { return "browser_hover" }

func (t *BrowserHoverTool) Description() string {
	return `Hover over an element on the page.

**When to use:**
- Triggering hover effects
- Revealing hidden menus
- Previewing link destinations

**Parameters:**
- selector: CSS selector for the element (required)

Returns confirmation of hover.`
}

func (t *BrowserHoverTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"selector": map[string]interface{}{
				"type":        "string",
				"description": "CSS selector for the element to hover",
			},
		},
		"required": []string{"selector"},
	}
}

func (t *BrowserHoverTool) Execute(args map[string]interface{}) (string, error) {
	return t.execute(context.Background(), args)
}

func (t *BrowserHoverTool) execute(parent context.Context, args map[string]interface{}) (string, error) {
	selector, ok := args["selector"].(string)
	if !ok || selector == "" {
		return "", fmt.Errorf("selector is required")
	}

	browserCtx, err := t.manager.GetOrCreateBrowser()
	if err != nil {
		return "", err
	}
	ctx, cancel := browserActionContext(parent, browserCtx, defaultBrowserActionTimeout)
	defer cancel()

	script := fmt.Sprintf(`
		const el = document.querySelector('%s');
		if (el) {
			const event = new MouseEvent('mouseover', { bubbles: true });
			el.dispatchEvent(event);
		}
	`, strings.ReplaceAll(selector, "'", "\\'"))

	if err := chromedp.Run(ctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.Evaluate(script, nil),
	); err != nil {
		return "", browserActionError("hover over element", defaultBrowserActionTimeout, err)
	}

	return fmt.Sprintf("Hovered over: %s", selector), nil
}

type BrowserPressKeyTool struct {
	manager *BrowserManager
}

func NewBrowserPressKeyTool(manager *BrowserManager) *BrowserPressKeyTool {
	return &BrowserPressKeyTool{manager: manager}
}

func (t *BrowserPressKeyTool) Name() string { return "browser_press_key" }

func (t *BrowserPressKeyTool) Description() string {
	return `Press a keyboard key.

**When to use:**
- Keyboard shortcuts
- Navigation (arrow keys, Enter, etc.)
- Special key presses

**Parameters:**
- key: Key to press (required) - e.g., Enter, Escape, ArrowDown, Tab, or a character

Returns confirmation of key press.`
}

func (t *BrowserPressKeyTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"key": map[string]interface{}{
				"type":        "string",
				"description": "Key to press (Enter, Escape, ArrowDown, Tab, etc.)",
			},
		},
		"required": []string{"key"},
	}
}

func (t *BrowserPressKeyTool) Execute(args map[string]interface{}) (string, error) {
	return t.execute(context.Background(), args)
}

func (t *BrowserPressKeyTool) execute(parent context.Context, args map[string]interface{}) (string, error) {
	key, ok := args["key"].(string)
	if !ok || key == "" {
		return "", fmt.Errorf("key is required")
	}

	browserCtx, err := t.manager.GetOrCreateBrowser()
	if err != nil {
		return "", err
	}
	ctx, cancel := browserActionContext(parent, browserCtx, defaultBrowserActionTimeout)
	defer cancel()

	keyMap := map[string]string{
		"Enter":      "\r",
		"Escape":     "\x1b",
		"Tab":        "\t",
		"Backspace":  "\b",
		"ArrowUp":    "\x1b[A",
		"ArrowDown":  "\x1b[B",
		"ArrowLeft":  "\x1b[D",
		"ArrowRight": "\x1b[C",
	}

	keyToSend, ok := keyMap[key]
	if !ok {
		keyToSend = key
	}

	if err := chromedp.Run(ctx, chromedp.KeyEvent(keyToSend)); err != nil {
		return "", browserActionError("press key", defaultBrowserActionTimeout, err)
	}

	return fmt.Sprintf("Pressed key: %s", key), nil
}

func (t *BrowserNavigateTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	return t.execute(ctx, args)
}

func (t *BrowserClickTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	return t.execute(ctx, args)
}

func (t *BrowserTypeTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	return t.execute(ctx, args)
}

func (t *BrowserScreenshotTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	return t.execute(ctx, args)
}

func (t *BrowserSnapshotTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	return t.execute(ctx, args)
}

func (t *BrowserCloseTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	return t.ExecuteWithContext(ctx, args)
}

func (t *BrowserCloseTool) ExecuteWithContext(ctx context.Context, args map[string]interface{}) (string, error) {
	return t.Execute(args)
}

func (t *BrowserEvaluateTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	return t.execute(ctx, args)
}

func (t *BrowserWaitForTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	return t.execute(ctx, args)
}

func (t *BrowserScrollTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	return t.execute(ctx, args)
}

func (t *BrowserGetHTMLTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	return t.execute(ctx, args)
}

func (t *BrowserHoverTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	return t.execute(ctx, args)
}

func (t *BrowserPressKeyTool) ExecuteWithRole(ctx context.Context, args map[string]interface{}, role models.Role) (string, error) {
	return t.execute(ctx, args)
}
