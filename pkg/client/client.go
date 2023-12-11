package client

import (
	"bytes"
	"context"
	"fmt"
	"io"
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

func SendWebhookData(ctx context.Context, urlPath string, headers map[string]string, data []byte) error {

	client := &http.Client{}

	ctx, cancel := context.WithTimeout(ctx, time.Second*3)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlPath, bytes.NewReader(data))
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rsp, err := client.Do(req)
	if err != nil {
		return err
	}

	response, _ := io.ReadAll(rsp.Body)
	if rsp.StatusCode != http.StatusOK {
		return fmt.Errorf("request to %s failed with response code: %d %v; headers sent: %v", urlPath, rsp.StatusCode, string(response), req.Header)
	}
	return nil
}
