package client

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"time"
)

const (
	HeaderNodeName     = "X-TinySystems-Node-Name"
	HeaderFlowID       = "X-TinySystems-Flow-Id"
	HeaderEdgeID       = "X-TinySystems-Edge-Id"
	HeaderPortFullName = "X-TinySystems-Port-Full-Name"
	HeaderError        = "X-TinySystems-Error"
)

func SendWebhookData(urlPath string, headers map[string]string, data []byte) error {

	client := &http.Client{}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlPath, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rsp, err := client.Do(req)
	if err != nil {
		return err
	}

	if rsp.StatusCode != http.StatusOK {
		return fmt.Errorf("request failed with response code: %d", rsp.StatusCode)
	}

	return nil
}
