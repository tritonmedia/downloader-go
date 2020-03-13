// Package process scans a dir for media files, and some pre-set conditions
// to determine which media files should have a job created for them to later
// be evaluated by Twilight
package process

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"
	"strings"
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

var allowedDirs = []string{
	"season",
}

var allowedDirsRegex = []*regexp.Regexp{
	regexp.MustCompile(`s\d+`),
}

// Dir finds media files in a dir and returns their paths
func Dir(path string) ([]string, error) {
	files := []string{}

	topLevelFiles, err := ioutil.ReadDir(path)
	if err != nil {
		return nil, err
	}

	topLevelDirs := make([]string, 0)
	allowed := allowedDirs
	for _, f := range topLevelFiles {
		if f.IsDir() {
			topLevelDirs = append(topLevelDirs, f.Name())
		}
	}

	// if we found one top-level directory, then we'll process it
	if len(topLevelDirs) == 1 {
		allowed = append(allowed, topLevelDirs...)
	}

	err = filepath.Walk(path, func(file string, info os.FileInfo, err error) error {
		// skip directories, unless they are allowed
		if info.IsDir() && file != path {
			dirName := filepath.Base(file)
			for _, dir := range allowed {
				// returning nil here makes us process the contents
				if strings.Contains(dirName, dir) {
					return nil
				}
			}

			// process regex filters
			for _, regex := range allowedDirsRegex {
				if regex.MatchString(dirName) {
					return nil
				}
			}
			return filepath.SkipDir
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
