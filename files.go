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
	Path       string        `json:"path"`
	FileCnt    int64         `json:"file_count"`
	FlacCnt    int64         `json:"flac_count"`
	TotalBytes int64         `json:"total_bytes"`
	Folders    []MusicFolder `json:"folders"`
}

type MusicFolder struct {
	Path       string      `json:"path"`
	HasAccurip bool        `json:"has_accurip"`
	TocID      string      `json:"toc_id"`
	Files      []MusicFile `json:"files"`
	FileCnt    int64       `json:"file_count"`
	FlacCnt    int64       `json:"flac_count"`
	TotalBytes int64       `json:"total_bytes"`
}

type MusicFile struct {
	Path     string   `json:"path"`
	Name     string   `json:"name"`
	Size     int64    `json:"size"`
	FileType FileType `json:"file_type"`
}

// ToCID returns the CueTools database lookup URL for the given TOC ID
func (mf MusicFolder) ToCID() string {
	return fmt.Sprintf(cueToolsLookupURL, mf.TocID)
}
