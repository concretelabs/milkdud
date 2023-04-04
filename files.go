package main

type FileType int

const (
	FileTypeFlac FileType = iota
	FileTypeLog
	FileTypeAccurip
	FileTypeJpg
	FileTypeJpeg
)

func (f FileType) String() string {
	return [...]string{"flac", "log", "accurip", "jpg", "jpeg"}[f]
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
