// Copyright 2026 Jayana Gunaweera (https://github.com/JayanaGunaweera01). All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package sloth is the lazy, fully local side of OpenAgent: a file tidier and a
// chat digester that run on an open-weight model served by Ollama. It depends on
// the standard library only, so it starts instantly and needs no database, login
// or network beyond localhost.
package sloth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	DefaultURL   = "http://localhost:11434"
	DefaultModel = "gemma4:e4b"
)

// Chatter is the one capability the sloth features need from a model.
type Chatter interface {
	Chat(ctx context.Context, system, user string, jsonMode bool) (string, error)
}

// Client talks to an Ollama server.
type Client struct {
	BaseURL string
	Model   string
	HTTP    *http.Client
}

// NewClient reads SLOTH_OLLAMA_URL / SLOTH_MODEL, falling back to sane local defaults.
func NewClient() *Client {
	url := strings.TrimRight(os.Getenv("SLOTH_OLLAMA_URL"), "/")
	if url == "" {
		url = DefaultURL
	}
	model := os.Getenv("SLOTH_MODEL")
	if model == "" {
		model = DefaultModel
	}
	return &Client{BaseURL: url, Model: model, HTTP: &http.Client{Timeout: 5 * time.Minute}}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string         `json:"model"`
	Messages []chatMessage  `json:"messages"`
	Stream   bool           `json:"stream"`
	Format   string         `json:"format,omitempty"`
	Options  map[string]any `json:"options,omitempty"`
}

type chatResponse struct {
	Message chatMessage `json:"message"`
	Error   string      `json:"error"`
}

// Chat sends one system+user exchange and returns the model's reply.
func (c *Client) Chat(ctx context.Context, system, user string, jsonMode bool) (string, error) {
	req := chatRequest{
		Model:    c.Model,
		Messages: []chatMessage{{Role: "system", Content: system}, {Role: "user", Content: user}},
		Stream:   false,
		Options:  map[string]any{"temperature": 0.2},
	}
	if jsonMode {
		req.Format = "json"
	}
	body, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return "", fmt.Errorf("cannot reach Ollama at %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	var out chatResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", fmt.Errorf("unexpected Ollama response (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || out.Error != "" {
		msg := out.Error
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return "", fmt.Errorf("ollama: %s", msg)
	}
	return strings.TrimSpace(out.Message.Content), nil
}

// Models lists the models pulled into the local Ollama server.
func (c *Client) Models(ctx context.Context) ([]string, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/api/tags", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("cannot reach Ollama at %s: %w", c.BaseURL, err)
	}
	defer resp.Body.Close()
	var tags struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(&tags); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		names = append(names, m.Name)
	}
	return names, nil
}
