package allowedext

import (
	_ "embed"
	"encoding/json"
	"strings"
	"sync"
)

//go:embed supported_file_types.json
var defaultData []byte

var (
	supported map[string]bool
	initOnce  sync.Once
)

func initMap() {
	var exts []string
	if err := json.Unmarshal(defaultData, &exts); err != nil {
		panic("allowedext: failed to parse supported_file_types.json: " + err.Error())
	}
	supported = make(map[string]bool, len(exts))
	for _, e := range exts {
		supported[strings.ToLower(e)] = true
	}
}

// IsAllowedExt returns true when the given file extension is in the supported types list.
// The check is case-insensitive.
func IsAllowedExt(ext string) bool {
	initOnce.Do(initMap)
	return supported[strings.ToLower(ext)]
}
