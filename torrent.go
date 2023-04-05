package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

type TorrentFile interface {
	AddFile(path string, size int64)
	Create(torrentFsRoot, outFile, comment string) (string, error)
}

type torrentFile struct {
	totalFileSizeBytes int64
	rootPath           string
	paths              map[string]int64
	announce           []string
}

// AddFile adds a file to the torrent file
func (tf *torrentFile) AddFile(path string, size int64) {
	relativePath, err := filepath.Rel(tf.rootPath, path)
	if err != nil {
		panic(err)
	}

	tf.paths[relativePath] = size
	tf.totalFileSizeBytes = tf.totalFileSizeBytes + size
}

// Create creates a torrent file and returns the magnet link
func (tf *torrentFile) Create(torrentFsRoot, outFile, comment string) (string, error) {

	mi := metainfo.MetaInfo{
		AnnounceList: [][]string{},
		Comment:      comment,
	}

	for _, tracker := range tf.announce {
		mi.AnnounceList = append(mi.AnnounceList, []string{tracker})
	}

	mi.SetDefaults()
	mi.CreatedBy = "milkdud"

	totalFileSizeBytes := int64(0)
	for _, size := range tf.paths {
		totalFileSizeBytes = totalFileSizeBytes + size
	}

	pieceLength := metainfo.ChoosePieceLength(totalFileSizeBytes)

	private := true
	info := metainfo.Info{
		PieceLength: pieceLength,
		Private:     &private,
	}

	info.Name = func() string {
		return torrentFsRoot
	}()

	info.Files = nil

	for path, size := range tf.paths {
		info.Length = size

		info.Files = append(info.Files, metainfo.FileInfo{
			Length: size,
			Path:   strings.Split(path, string(filepath.Separator)),
		})
	}

	if info.PieceLength == 0 {
		info.PieceLength = metainfo.ChoosePieceLength(info.TotalLength())
	}

	err := info.GeneratePieces(func(fi metainfo.FileInfo) (io.ReadCloser, error) {
		return os.Open(filepath.Join(tf.rootPath, strings.Join(fi.Path, string(filepath.Separator))))
	})

	if err != nil {
		return "", fmt.Errorf("error generating pieces: %s", err)
	}

	var bencodeErr error
	mi.InfoBytes, bencodeErr = bencode.Marshal(info)
	if bencodeErr != nil {
		return "", bencodeErr
	}

	magnetURL := mi.Magnet(nil, nil)

	f, openErr := os.OpenFile(outFile, os.O_WRONLY|os.O_CREATE|os.O_CREATE, 0600)
	if openErr != nil {
		return "", openErr
	}
	defer f.Close()

	outErr := mi.Write(f)
	if outErr != nil {
		return "", outErr
	}

	return magnetURL.String(), nil
}

// createTorrentFile creates a new TorrentFile
func createTorrentFile(rootPath string, announce []string) (TorrentFile, error) {

	tf := torrentFile{
		paths:    map[string]int64{},
		rootPath: rootPath,
		announce: announce,
	}

	return &tf, nil
}
