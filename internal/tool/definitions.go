package tool

import "fmt"

// ToolName enumerates all available code review tools.
// name: original name (XML-compatible), alias: clean name (function-call compatible).
type Tool struct {
	Name  string // original name, used in XML-format tool calls
	Alias string // clean alias, used in native function calls (no special chars)
}

var (
	Unknown   = Tool{Name: "unknown", Alias: "unknown"}
	TaskDone  = Tool{Name: "task_done", Alias: "task_done"}
	CodeComment = Tool{Name: "code_comment", Alias: "code_comment"}
	FileRead    = Tool{Name: "file.read", Alias: "file_read"}
	FileFind    = Tool{Name: "file.find", Alias: "file_find"}
	FileReadDiff = Tool{Name: "file.read_diff", Alias: "file_read_diff"}
	FileSearch   = Tool{Name: "file.search", Alias: "file_search"}
	CodeSearch   = Tool{Name: "code.search", Alias: "code_search"}
)

func OfAlias(alias string) Tool {
	for _, t := range allTools() {
		if t.Alias == alias {
			return t
		}
	}
	return Unknown
}

func OfName(name string) Tool {
	for _, t := range allTools() {
		if t.Name == name {
			return t
		}
	}
	return Unknown
}

func allTools() []Tool {
	return []Tool{Unknown, TaskDone, CodeComment, FileRead, FileFind, FileReadDiff, FileSearch, CodeSearch}
}

// IsKnown reports whether the tool is not UNKNOWN.
func (t Tool) IsKnown() bool {
	return t != Unknown
}

// LookupResult holds the result of a single tool lookup.
type LookupResult struct {
	Result     string
	Found      bool
}

// Provider is the interface that all concrete tool implementations satisfy.
// Each tool handles one specific capability (read file, search code, etc.).
type Provider interface {
	// Tool returns which tool this provider implements.
	Tool() Tool
	// Execute runs the tool with the given arguments and returns the result string.
	Execute(args map[string]any) (string, error)
}

// Registry maps tool aliases to their providers. Users register their own implementations here.
type Registry map[string]Provider

// NewRegistry creates an empty registry.
func NewRegistry() Registry {
	return make(Registry)
}

// Register adds a tool provider to the registry.
func (r Registry) Register(p Provider) {
	r[p.Tool().Alias] = p
}

// Lookup finds a provider by alias. Returns a zero-value LookupResult if not found.
func (r Registry) Lookup(alias string) LookupResult {
	p, ok := r[alias]
	if !ok {
		return LookupResult{Found: false}
	}
	return LookupResult{Result: p.Tool().Alias, Found: true}
}

// ErrToolNotFound is returned when a tool alias cannot be resolved.
var ErrToolNotFound = fmt.Errorf("tool not found")

// NotAvailableError is the standard message returned when a tool is not registered.
const NotAvailableMsg = "Error: Tool not found. The tool you attempted to call does not exist or is not available. Please check the tool name and try again with a valid tool."
