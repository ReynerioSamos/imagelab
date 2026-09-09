package main

import "os"

// ensureStorageDirs creates the on-disk layout ImageLab writes to:
// storage/originals for uploaded files , storage/variants for
// worker-generated output.
func ensureStorageDirs(root string) error {
	dirs := []string{
		root,
		root + "/originals",
		root + "/variants",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
