package main

import "fmt"

type FileType string

// cueToolsLookupURL is the URL to the CueTools database lookup page
const cueToolsLookupURL = "http://db.cuetools.net/top.php?tocid=%s"

const (
	FileTypeFlac    FileType = "flac"
	FileTypeLog     FileType = "log"
	FileTypeAccurip FileType = "accurip"
	FileTypeJpg     FileType = "jpg"
	FileTypeJpeg    FileType = "jpeg"
)

func (ft FileType) String() string {
	return string(ft)
}

type MusicLibrary struct {
	Path       string
	FileCnt    int64
	FlacCnt    int64
	TotalBytes int64
	Folders    []MusicFolder
}

type MusicFolder struct {
	Path       string
	HasAccurip bool
	TocID      string
	Files      []MusicFile
	FileCnt    int64
	FlacCnt    int64
	TotalBytes int64
}

type MusicFile struct {
	Path     string
	Name     string
	Size     int64
	FileType FileType
}

// ToCID returns the CueTools database lookup URL for the given TOC ID
func (mf MusicFolder) ToCID() string {
	return fmt.Sprintf(cueToolsLookupURL, mf.TocID)
}
