// Package client sends data to another device's server.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"dsync/internal/proto"
)

var httpClient = &http.Client{Timeout: 10 * time.Second}

func SendText(ctx context.Context, addr string, m proto.TextMessage) error {
	body, _ := json.Marshal(m)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+"/api/v1/text", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	return nil
}

// Info asks a device to describe itself.
func Info(ctx context.Context, addr string) (proto.Device, error) {
	var d proto.Device
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/api/v1/info", nil)
	if err != nil {
		return d, err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return d, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return d, fmt.Errorf("%s", resp.Status)
	}
	err = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&d)
	return d, err
}
