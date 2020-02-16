// Package process scans a dir for media files, and some pre-set conditions
// to determine which media files should have a job created for them to later
// be evaluated by Twilight
package process

import (
	"os"
	"path/filepath"
)

// TODO(jaredallard): running files through ffprobe is likely to end up
// working better than file extensions
// valid media file extensions
var mediaExts = map[string]bool{
	".mp4":  true,
	".mkv":  true,
	".mov":  true,
	".webm": true,
}

// Dir finds media files in a dir and returns their paths
func Dir(path string) ([]string, error) {
	files := []string{}

	// TODO(jaredallard): add filtering dirs we "enter"
	err := filepath.Walk(path, func(file string, info os.FileInfo, err error) error {
		// skip directories
		if info.IsDir() {
			return nil
		}

		// walk had an error :( so we die
		if err != nil {
			return err
		}

		ext := filepath.Ext(file)
		if !mediaExts[ext] {
			return nil
		}

		files = append(files, file)

		return nil
	})
	if err != nil {
		return nil, err
	}

	return files, nil
}
