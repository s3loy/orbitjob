package main

import (
	"bytes"
	"fmt"
	"os"
)

// A run's generated corpus is immutable, including images and workload bodies.
// Prepared revision IDs refer to this exact corpus; even an unstarted run must
// not silently replace it while retaining references to the previous revisions.
func writeGeneratedCorpus(path string, definitions []Definition) error {
	data := mustJSON(definitions)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		saved, err := loadGeneratedDefinitions(path)
		if err != nil {
			return fmt.Errorf("read existing generated corpus: %w", err)
		}
		if !bytes.Equal(mustJSON(saved), data) {
			return fmt.Errorf("generated corpus differs from this run's saved images/scenarios/specs; use a new run ID")
		}
		return nil
	}
	if err != nil {
		return err
	}
	_, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil || closeErr != nil {
		return fmt.Errorf("persist generated corpus: write=%v close=%v", writeErr, closeErr)
	}
	return nil
}
