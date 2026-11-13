package rar

import (
	"fmt"
	"testing"

	"github.com/kipsilabs/altmount/internal/importer/parser"
)

func volumeSet(head string, base string, count int) []parser.ParsedFile {
	files := []parser.ParsedFile{}
	if head != "" {
		files = append(files, parser.ParsedFile{Filename: head})
	}
	for i := 0; i < count; i++ {
		files = append(files, parser.ParsedFile{Filename: fmt.Sprintf("%s.r%02d", base, i)})
	}
	return files
}

func TestUnifyRenamedHeadVolumeRepairsRenamedFirstVolume(t *testing.T) {
	for _, tc := range []struct{ head, base string }{
		// Real indexer sets that stopped after one volume.
		{"Farmshow.S14E04.DANiSH.PDTV.x264-GRPB.part01.rar", "farmshow.s14e04.danish.pdtv.x264-grpb"},
		{"Farmshow.S11.DANiSH.PDTV.x264-SCENE.part01.rar", "scene-farmshow.s11.pdtv"},
		{"Farmshow.S02.DANISH.DVDRip-GRPA.part01.rar", "grpa-farmshow.s02.dvdrip"},
		{"Show.S01.rar", "grp-show.s01"},
	} {
		files := volumeSet(tc.head, tc.base, 29)
		unified, changed := unifyRenamedHeadVolume(files)
		if !changed || unified[0].Filename != tc.base+".rar" {
			t.Fatalf("%s: changed=%v head=%q", tc.head, changed, unified[0].Filename)
		}
		if files[0].Filename != tc.head {
			t.Fatal("input slice was modified")
		}
		for i := 1; i < len(unified); i++ {
			if unified[i].Filename != files[i].Filename {
				t.Fatalf("volume %d renamed: %q", i, unified[i].Filename)
			}
		}
		if next := nextOldVolNameForTest(unified[0].Filename); next != tc.base+".r00" {
			t.Fatalf("old-style follow from %q gives %q", unified[0].Filename, next)
		}
	}
}

func TestUnifyRenamedHeadVolumeLeavesOtherSetsAlone(t *testing.T) {
	newStyle := []parser.ParsedFile{}
	for i := 1; i <= 5; i++ {
		newStyle = append(newStyle, parser.ParsedFile{Filename: fmt.Sprintf("Movie.part%02d.rar", i)})
	}
	gap := volumeSet("Show.part01.rar", "show", 6)
	gap = append(gap[:3], gap[4:]...) // r02 missing
	twoRuns := append(volumeSet("Show.part01.rar", "a-show", 3), volumeSet("", "b-show", 3)...)
	twoHeads := append(volumeSet("Show.part01.rar", "show", 3), parser.ParsedFile{Filename: "Other.rar"})
	notFromZero := volumeSet("Show.part01.rar", "show", 4)[0:1]
	notFromZero = append(notFromZero, parser.ParsedFile{Filename: "show.r01"}, parser.ParsedFile{Filename: "show.r02"})
	for name, files := range map[string][]parser.ParsedFile{
		"new-style set":        newStyle,
		"already consistent":   volumeSet("show.rar", "show", 5),
		"gap in run":           gap,
		"two .rNN runs":        twoRuns,
		"two heads":            twoHeads,
		"run not from r00":     notFromZero,
		"no head":              volumeSet("", "show", 5),
		"mixed padding widths": append(volumeSet("Show.part01.rar", "show", 2), parser.ParsedFile{Filename: "show.r002"}),
	} {
		if _, changed := unifyRenamedHeadVolume(files); changed {
			t.Fatalf("%s: set was changed", name)
		}
	}
}

// nextOldVolNameForTest mirrors rardecode's old-style successor for "x.rar".
func nextOldVolNameForTest(name string) string {
	return name[:len(name)-len(".rar")] + ".r00"
}

func sizedSet(head string, headSize int64, base string, count int, volumeSize int64) []parser.ParsedFile {
	files := []parser.ParsedFile{{Filename: head, Size: headSize}}
	for i := 0; i < count; i++ {
		size := volumeSize
		if i == count-1 {
			size = volumeSize / 3 // last volume is short
		}
		files = append(files, parser.ParsedFile{Filename: fmt.Sprintf("%s.r%02d", base, i), Size: size})
	}
	return files
}

func TestGroupingMergesCleanNamedHeadBySize(t *testing.T) {
	// Real indexer layout: clean release name on the head, scene name on .rNN.
	files := sizedSet("Farmshow.S02.DANISH.DVDRip-GRPA.part01.rar", 207_700_000, "grpa-farmshow.s02.dvdrip", 39, 207_650_000)
	groups := GroupArchivesByBaseName(files)
	if len(groups) != 1 || len(groups[0]) != 40 || groups[0][0].Filename != "grpa-farmshow.s02.dvdrip.rar" || groups[0][1].Filename != "grpa-farmshow.s02.dvdrip.r00" {
		t.Fatalf("set not merged: %d groups, first %q", len(groups), groups[0][0].Filename)
	}
}

func TestGroupingKeepsUnrelatedArchivesApart(t *testing.T) {
	for name, files := range map[string][]parser.ParsedFile{
		"different size":   sizedSet("Extras.part01.rar", 50_000_000, "grp-show.s02", 10, 207_650_000),
		"too few volumes":  sizedSet("Show.part01.rar", 207_650_000, "grp-show.s02", 2, 207_650_000),
		"unknown sizes":    sizedSet("Show.part01.rar", 0, "grp-show.s02", 10, 207_650_000),
		"gap in the run":   append(sizedSet("Show.part01.rar", 207_650_000, "grp-show.s02", 4, 207_650_000)[:3], parser.ParsedFile{Filename: "grp-show.s02.r05", Size: 207_650_000}),
		"run not from r00": sizedSet("Show.part01.rar", 207_650_000, "grp-show.s02", 6, 207_650_000)[0:1],
	} {
		if name == "run not from r00" {
			for i := 1; i <= 5; i++ {
				files = append(files, parser.ParsedFile{Filename: fmt.Sprintf("grp-show.s02.r%02d", i), Size: 207_650_000})
			}
		}
		if groups := GroupArchivesByBaseName(files); len(groups) < 2 {
			t.Fatalf("%s: unrelated archives merged into %d group", name, len(groups))
		}
	}
}
