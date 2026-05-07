package tools

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/chromedp/chromedp"
)

type BrowserManager struct {
	mu            sync.Mutex
	allocatorCtx  context.Context
	allocatorCancel context.CancelFunc
	browserCtx    context.Context
	browserCancel context.CancelFunc
	currentURL    string
}

func NewBrowserManager() *BrowserManager {
	return &BrowserManager{}
}

func (m *BrowserManager) GetOrCreateBrowser() (context.Context, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.browserCtx != nil {
		return m.browserCtx, nil
	}

	if m.allocatorCtx == nil {
		opts := append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", true),
			chromedp.Flag("disable-gpu", true),
			chromedp.WindowSize(1280, 720),
		)
		m.allocatorCtx, m.allocatorCancel = chromedp.NewExecAllocator(context.Background(), opts...)
	}

	m.browserCtx, m.browserCancel = chromedp.NewContext(m.allocatorCtx)
	if err := chromedp.Run(m.browserCtx); err != nil {
		m.browserCtx = nil
		m.browserCancel = nil
		return nil, fmt.Errorf("failed to start browser: %w", err)
	}

	return m.browserCtx, nil
}

func (m *BrowserManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.browserCancel != nil {
		m.browserCancel()
		m.browserCtx = nil
		m.browserCancel = nil
	}
	if m.allocatorCancel != nil {
		m.allocatorCancel()
		m.allocatorCtx = nil
		m.allocatorCancel = nil
	}
}

func (m *BrowserManager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.browserCtx != nil
}

func (m *BrowserManager) GetCurrentURL() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.currentURL
}

func (m *BrowserManager) SetCurrentURL(url string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentURL = url
}

func (m *BrowserManager) Navigate(url string) error {
	ctx, err := m.GetOrCreateBrowser()
	if err != nil {
		return err
	}

	if err := chromedp.Run(ctx, chromedp.Navigate(url)); err != nil {
		return fmt.Errorf("navigate failed: %w", err)
	}

	m.SetCurrentURL(url)
	return nil
}

func (m *BrowserManager) Click(selector string) error {
	ctx, err := m.GetOrCreateBrowser()
	if err != nil {
		return err
	}

	return chromedp.Run(ctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.Click(selector, chromedp.ByQuery),
	)
}

func (m *BrowserManager) Type(selector, text string) error {
	ctx, err := m.GetOrCreateBrowser()
	if err != nil {
		return err
	}

	return chromedp.Run(ctx,
		chromedp.WaitVisible(selector, chromedp.ByQuery),
		chromedp.SendKeys(selector, text, chromedp.ByQuery),
	)
}

func (m *BrowserManager) Screenshot() ([]byte, error) {
	ctx, err := m.GetOrCreateBrowser()
	if err != nil {
		return nil, err
	}

	var buf []byte
	if err := chromedp.Run(ctx, chromedp.FullScreenshot(&buf, 90)); err != nil {
		return nil, fmt.Errorf("screenshot failed: %w", err)
	}
	return buf, nil
}

func (m *BrowserManager) GetHTML(selector string) (string, error) {
	ctx, err := m.GetOrCreateBrowser()
	if err != nil {
		return "", err
	}

	var html string
	if selector == "" {
		selector = "body"
	}
	if err := chromedp.Run(ctx, chromedp.OuterHTML(selector, &html, chromedp.ByQuery)); err != nil {
		return "", fmt.Errorf("get html failed: %w", err)
	}
	return html, nil
}

func (m *BrowserManager) GetText(selector string) (string, error) {
	ctx, err := m.GetOrCreateBrowser()
	if err != nil {
		return "", err
	}

	var text string
	if selector == "" {
		selector = "body"
	}
	if err := chromedp.Run(ctx, chromedp.Text(selector, &text, chromedp.ByQuery)); err != nil {
		return "", fmt.Errorf("get text failed: %w", err)
	}
	return text, nil
}

func (m *BrowserManager) Evaluate(js string) (interface{}, error) {
	ctx, err := m.GetOrCreateBrowser()
	if err != nil {
		return nil, err
	}

	var result interface{}
	if err := chromedp.Run(ctx, chromedp.Evaluate(js, &result)); err != nil {
		return nil, fmt.Errorf("evaluate failed: %w", err)
	}
	return result, nil
}

func (m *BrowserManager) WaitFor(selector string, timeout time.Duration) error {
	ctx, err := m.GetOrCreateBrowser()
	if err != nil {
		return err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return chromedp.Run(timeoutCtx, chromedp.WaitVisible(selector, chromedp.ByQuery))
}

func (m *BrowserManager) WaitForText(text string, timeout time.Duration) error {
	ctx, err := m.GetOrCreateBrowser()
	if err != nil {
		return err
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	script := fmt.Sprintf(`new Promise((resolve) => {
		if (document.body.innerText.includes('%s')) {
			resolve(true);
		} else {
			const observer = new MutationObserver(() => {
				if (document.body.innerText.includes('%s')) {
					observer.disconnect();
					resolve(true);
				}
			});
			observer.observe(document.body, { childList: true, subtree: true });
		}
	})`, text, text)

	return chromedp.Run(timeoutCtx, chromedp.Evaluate(script, nil))
}