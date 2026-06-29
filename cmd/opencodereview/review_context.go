package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const maxReviewContextBytes = 1_000_000

type reviewContextFile struct {
	Version int `json:"version"`
	Paths   map[string]struct {
		Rendered string `json:"rendered"`
	} `json:"paths"`
}

func loadReviewContext(path string) (map[string]string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat review context: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("review context must be a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read review context: %w", err)
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxReviewContextBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read review context: %w", err)
	}
	if len(data) > maxReviewContextBytes {
		return nil, fmt.Errorf("review context is too large (limit %d bytes)", maxReviewContextBytes)
	}
	var payload reviewContextFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("parse review context: %w", err)
	}
	if payload.Version != 1 {
		return nil, fmt.Errorf("review context version %d is unsupported", payload.Version)
	}
	if len(payload.Paths) == 0 {
		return nil, nil
	}
	out := make(map[string]string, len(payload.Paths))
	for path, entry := range payload.Paths {
		if strings.TrimSpace(path) == "" || path != strings.TrimSpace(path) || strings.TrimSpace(entry.Rendered) == "" {
			return nil, fmt.Errorf("review context path %q is malformed", path)
		}
		out[path] = entry.Rendered
	}
	return out, nil
}
