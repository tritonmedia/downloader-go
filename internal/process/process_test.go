// Package process scans a dir for media files, and some pre-set conditions
// to determine which media files should have a job created for them to later
// be evaluated by Twilight
package process

import (
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

func TestDir(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	testdataDir := filepath.Join(filepath.Dir(filename), "testdata")
	type args struct {
		path string
	}
	tests := []struct {
		name    string
		args    args
		want    []string
		wantErr bool
	}{
		{
			name: "should find a movie",
			args: args{
				path: filepath.Join(testdataDir, "movie"),
			},
			want: []string{filepath.Join(testdataDir, "movie/movie.mkv")},
		},
		{
			name: "should find a movie in a top level directory",
			args: args{
				path: filepath.Join(testdataDir, "movie-tld"),
			},
			want: []string{filepath.Join(testdataDir, "movie-tld/movie/movie.mkv")},
		},
		{
			name: "should find files in sub directories",
			args: args{
				path: filepath.Join(testdataDir, "seasons-subdir"),
			},
			want: []string{
				filepath.Join(testdataDir, "seasons-subdir/season 1/e1.mkv"),
				filepath.Join(testdataDir, "seasons-subdir/season 2/e1.mkv"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Dir(tt.args.path)
			if (err != nil) != tt.wantErr {
				t.Errorf("Dir() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Dir() = %v, want %v", got, tt.want)
			}
		})
	}
}
