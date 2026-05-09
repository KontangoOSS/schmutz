package discover

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type Publisher struct {
	schmutzDir string
	baoAddr    string
	baoToken   string
}

func NewPublisher(schmutzDir, baoAddr, baoToken string) *Publisher {
	return &Publisher{schmutzDir: schmutzDir, baoAddr: baoAddr, baoToken: baoToken}
}

func (p *Publisher) PublishLocal(result *DiscoveryResult) error {
	data, err := result.MarshalToJSON()
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	if err := os.WriteFile(filepath.Join(p.schmutzDir, "schema.json"), data, 0600); err != nil {
		return fmt.Errorf("write schema.json: %w", err)
	}
	return nil
}

func (p *Publisher) PublishBao(result *DiscoveryResult) {
	if p.baoAddr == "" || p.baoToken == "" {
		return
	}
	data, err := result.MarshalToJSON()
	if err != nil {
		log.Printf("discover: bao marshal error: %v", err)
		return
	}
	payload := map[string]interface{}{
		"data": map[string]interface{}{
			"schema": string(data), "discovered_at": result.DiscoveredAt.Format(time.RFC3339), "slug": result.Slug,
		},
	}
	body, _ := json.Marshal(payload)
	req, err := http.NewRequest(http.MethodPost,
		fmt.Sprintf("%s/v1/secret/data/services/%s/schema", p.baoAddr, result.Slug),
		bytes.NewReader(body))
	if err != nil {
		log.Printf("discover: bao request error: %v", err)
		return
	}
	req.Header.Set("X-Vault-Token", p.baoToken)
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		log.Printf("discover: bao unreachable (%v) — schema saved locally only", err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		log.Printf("discover: bao returned %d — schema saved locally only", resp.StatusCode)
	}
}

func (p *Publisher) Publish(result *DiscoveryResult) error {
	if err := p.PublishLocal(result); err != nil {
		return err
	}
	go p.PublishBao(result)
	return nil
}
