package parser

import (
	"context"
	"testing"

	"github.com/javi11/nzbparser"
	"github.com/kipsilabs/altmount/internal/importer/parser/fileinfo"
)

func TestSingleFileMetadataPreservesOriginalNameWithoutExtensionOverride(t *testing.T) {
	for _, test := range []struct {
		name, extension, want string
	}{
		{"Original.Movie.mkv", "", "Original.Movie.mkv"},
		{"Original.Movie.mkv", ".mp4", "owner-upload.mp4"},
		{"Original.Movie.mkv", ".mkv", "owner-upload.mkv"},
	} {
		t.Run(test.name+test.extension, func(t *testing.T) {
			meta := map[string]string{"file_name": test.name, "file_size": "128"}
			if test.extension != "" {
				meta["file_extension"] = test.extension
			}
			info := &fileinfo.FileInfo{Filename: test.name, NzbFile: nzbparser.NzbFile{
				Segments: nzbparser.NzbSegments{{ID: "fixture", Number: 1, Bytes: 128}},
			}}
			p := NewParser(nil, testConfigGetter())
			file, err := p.parseFile(context.Background(), meta, "owner-upload.nzb", info, nil, nil, 0, nil, nil)
			if err != nil || file == nil {
				t.Fatalf("parseFile=%v error=%v", file, err)
			}
			if file.Filename != test.want || file.Size != 128 {
				t.Fatalf("filename=%q size=%d, want %q size=128", file.Filename, file.Size, test.want)
			}
		})
	}
}
