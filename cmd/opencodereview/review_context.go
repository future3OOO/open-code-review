package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/open-code-review/open-code-review/internal/model"
)

const maxReviewContextBytes = 1_000_000

type reviewContextFile struct {
	Version    int                    `json:"version"`
	Revalidate []reviewContextFinding `json:"revalidate"`
	Paths      map[string]struct {
		Rendered string `json:"rendered"`
	} `json:"paths"`
}

type reviewContextFinding struct {
	model.LlmComment
	InstanceID string `json:"instance_id"`
}

type reviewContextInput struct {
	Paths      map[string]string
	Revalidate []model.LlmComment
}

func loadReviewContext(path string) (reviewContextInput, error) {
	if strings.TrimSpace(path) == "" {
		return reviewContextInput{}, nil
	}
	file, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return reviewContextInput{}, fmt.Errorf("read review context: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return reviewContextInput{}, fmt.Errorf("stat review context: %w", err)
	}
	if !info.Mode().IsRegular() {
		return reviewContextInput{}, fmt.Errorf("review context must be a regular file")
	}
	data, err := io.ReadAll(io.LimitReader(file, maxReviewContextBytes+1))
	if err != nil {
		return reviewContextInput{}, fmt.Errorf("read review context: %w", err)
	}
	if len(data) > maxReviewContextBytes {
		return reviewContextInput{}, fmt.Errorf("review context is too large (limit %d bytes)", maxReviewContextBytes)
	}
	var payload reviewContextFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return reviewContextInput{}, fmt.Errorf("parse review context: %w", err)
	}
	if payload.Version != 1 {
		return reviewContextInput{}, fmt.Errorf("review context version %d is unsupported", payload.Version)
	}
	out := reviewContextInput{}
	if len(payload.Paths) > 0 {
		out.Paths = make(map[string]string, len(payload.Paths))
	}
	for path, entry := range payload.Paths {
		if strings.TrimSpace(path) == "" || path != strings.TrimSpace(path) || strings.TrimSpace(entry.Rendered) == "" {
			return reviewContextInput{}, fmt.Errorf("review context path %q is malformed", path)
		}
		out.Paths[path] = entry.Rendered
	}
	for index, finding := range payload.Revalidate {
		if malformedRevalidation(finding) {
			return reviewContextInput{}, fmt.Errorf("review context revalidation finding %d is malformed", index)
		}
		out.Revalidate = append(out.Revalidate, finding.LlmComment)
	}
	return out, nil
}

func malformedRevalidation(finding reviewContextFinding) bool {
	required := []string{
		finding.InstanceID,
		finding.Path,
		finding.Content,
		finding.FailureMode,
		finding.ViolatedContract,
		finding.Evidence,
		finding.ExistingCode,
	}
	for _, value := range required {
		if strings.TrimSpace(value) == "" {
			return true
		}
	}
	if finding.Path != strings.TrimSpace(finding.Path) || strings.HasPrefix(finding.Path, "/") || finding.StartLine < 0 || finding.EndLine < 0 {
		return true
	}
	for _, part := range strings.Split(finding.Path, "/") {
		if part == "" || part == ".." {
			return true
		}
	}
	if finding.StartLine > 0 && finding.EndLine > 0 && finding.EndLine < finding.StartLine {
		return true
	}
	return finding.Severity != "critical" && finding.Severity != "high" && finding.Severity != "medium"
}
