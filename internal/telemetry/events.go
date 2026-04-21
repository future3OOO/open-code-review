package telemetry

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Event emits a structured event as a span with immediate end.
func Event(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	if !IsEnabled() || ctx == nil { return }

	opts := []trace.SpanStartOption{trace.WithAttributes(attrs...)}
	_, span := otel.GetTracerProvider().Tracer(serviceName).Start(ctx, "event."+name, opts...)
	defer span.End()
}

// Eventf is like Event but includes a message attribute.
func Eventf(ctx context.Context, name string, msg string, attrs ...attribute.KeyValue) {
	Event(ctx, name, append(attrs, attribute.String("message", msg))...)
}

// ErrorEvent emits an error event with error status.
func ErrorEvent(ctx context.Context, name string, err error, extraAttrs ...attribute.KeyValue) {
	if !IsEnabled() || ctx == nil || err == nil { return }

	attrs := append(extraAttrs, attribute.String("error", err.Error()))
	_, span := otel.GetTracerProvider().Tracer(serviceName).Start(ctx, "event."+name,
		trace.WithAttributes(attrs...))
	span.SetStatus(codes.Error, err.Error())
	span.RecordError(err)
	span.End()
}

// PhaseEvent records a review phase completion with duration and optional error.
func PhaseEvent(ctx context.Context, phase string, filePath string, dur time.Duration, err error) {
	attrs := []attribute.KeyValue{
		attribute.String("phase", phase),
		attribute.String("file.path", filePath),
		attribute.Int64("duration_ms", dur.Milliseconds()),
	}
	if err != nil {
		ErrorEvent(ctx, "phase.completed", err, attrs...)
	} else {
		Event(ctx, "phase.completed", attrs...)
	}
}

// FormatDuration returns a human-readable duration string for console output.
func FormatDuration(dur time.Duration) string {
	return dur.Round(time.Millisecond).String()
}

// PrintTraceSummary prints a one-line summary of the review to stdout.
func PrintTraceSummary(filesReviewed, commentsGenerated int64, totalTokens int64, duration time.Duration) {
	fmt.Printf("[argus] Summary: %d file(s) reviewed, %d comment(s), ~%d token(s) used, %s elapsed\n",
		filesReviewed, commentsGenerated, totalTokens, FormatDuration(duration))
}

// PrintToolCallStarted prints a line when a tool begins execution.
// Args are summarized as key-value pairs (path, search terms, etc.).
// Example: [argus]   ▶ file_read "internal/config/rules/loader.go"
func PrintToolCallStarted(toolName string, args map[string]any) {
	summary := summarizeArgs(args)
	if summary != "" {
		fmt.Printf("[argus]   ▶ %s %s\n", toolName, summary)
	} else {
		fmt.Printf("[argus]   ▶ %s\n", toolName)
	}
}

// PrintToolCallFinished prints a line when a tool finishes successfully.
// Example: [argus]   ✔ file_read "internal/config/rules/loader.go" (12ms)
func PrintToolCallFinished(toolName string, dur time.Duration) {
	fmt.Printf("[argus]   ✔ %s (%s)\n", toolName, FormatDuration(dur))
}

// PrintToolCallError prints a line when a tool fails.
// Example: [argus]   ✘ file_read "internal/config/rules/loader.go" failed: permission denied
func PrintToolCallError(toolName string, err error) {
	fmt.Fprintf(os.Stderr, "[argus]   ✘ %s failed: %v\n", toolName, err)
}

// summarizeArgs extracts a concise key=value summary from tool arguments for console display.
// It picks the most human-readable fields depending on the argument keys.
func summarizeArgs(args map[string]any) string {
	parts := make([]string, 0, len(args))
	for k, v := range args {
		s := fmt.Sprint(v)
		switch k {
		case "path":
			return strconv.Quote(s)
		case "search", "query", "pattern":
			return strconv.Quote(s)
		default:
			if len(s) <= 50 {
				parts = append(parts, fmt.Sprintf("%s=%s", k, s))
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " ")
}
