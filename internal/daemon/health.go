package daemon

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

type HealthChecker struct {
	endpoint string
	client   *http.Client
}

func NewHealthChecker(endpoint string) *HealthChecker {
	return &HealthChecker{
		endpoint: endpoint,
		client:   &http.Client{Timeout: 5 * time.Second},
	}
}

func (hc *HealthChecker) Check() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", hc.endpoint, nil)
	if err != nil {
		return err
	}

	resp, err := hc.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &HealthError{Status: resp.StatusCode}
	}

	return nil
}

type HealthError struct {
	Status int
}

func (e *HealthError) Error() string {
	return fmt.Sprintf("health check failed: status %d", e.Status)
}
