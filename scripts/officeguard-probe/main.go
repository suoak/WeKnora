// officeguard-probe reports detector results without persisting source contents.
package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/Tencent/WeKnora/internal/officeguard"
)

func main() {
	if len(os.Args) < 2 {
		os.Exit(2)
	}
	failed := false
	for _, path := range os.Args[1:] {
		f, err := os.Open(path)
		if err != nil {
			failed = true
			continue
		}
		info, err := f.Stat()
		if err != nil {
			_ = f.Close()
			failed = true
			continue
		}
		result := officeguard.Detect(context.Background(), f, info.Size(), info.Name())
		_ = f.Close()
		if err := json.NewEncoder(os.Stdout).Encode(map[string]any{
			"file": filepath.Base(path), "bytes": info.Size(), "classification": result,
		}); err != nil {
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}
