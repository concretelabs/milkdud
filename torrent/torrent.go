package torrent

import (
	"crypto/sha1"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/anacrolix/missinggo/v2/slices"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"golang.org/x/sync/errgroup"
)

const torrentFsBase = "music"

type TorrentFile interface {
	AddFile(path string, size int64)
	Create(outFile string) error
	MagnetURL() string
}

type torrentFile struct {
	// t                  torrent
	totalFileSizeBytes int64
	root               string
	paths              map[string]int64
	files              []metainfo.FileInfo
	announce           []string
	mi                 *metainfo.MetaInfo
	logOutput          bool
}

// AddFile adds a file to the torrent
func (tf *torrentFile) AddFile(path string, size int64) {
	relativePath, err := filepath.Rel(tf.root, path)
	if err != nil {
		panic(err)
	}

	tf.paths[relativePath] = size
	tf.totalFileSizeBytes = tf.totalFileSizeBytes + size

	tf.files = append(tf.files, metainfo.FileInfo{
		Length:   size,
		Path:     []string{path},
		PathUtf8: []string{path},
	})
}

func (tf *torrentFile) buildFromPathList(info metainfo.Info) (metainfo.Info, error) {

	info.Name = func() string {
		// b := filepath.Base(tf.root)
		// switch b {
		// case ".", "..", string(filepath.Separator):
		// 	return metainfo.NoName
		// default:
		// 	return b
		// }
		return torrentFsBase
	}()

	info.Files = nil

	for _, file := range tf.files {

		if len(file.Path) > 0 {

			if len(file.Path) > 1 {
				if tf.logOutput {
					log.Println("ignoring multiple paths for file")
				}
				continue
			}

			path := file.Path[0]

			fi, statErr := os.Stat(path)
			if os.IsNotExist(statErr) || len(path) == 0 {
				return info, fmt.Errorf("path doesn't exist %s", statErr)
			}

			if path == tf.root {
				info.Length = fi.Size()
				return info, nil
			}

			relPath, err := filepath.Rel(tf.root, path)
			if err != nil {
				return info, fmt.Errorf("error getting relative path: %s", err)
			}

			info.Files = append(info.Files, metainfo.FileInfo{
				Path:   strings.Split(relPath, string(filepath.Separator)),
				Length: fi.Size(),
			})
		}
	}

	return info, nil
}

// Create creates a torrent file
func (tf *torrentFile) Create(outFile string) error {
	startTime := time.Now()

	if tf.logOutput {
		fmt.Println("Creating torrent file", outFile)
	}

	pieceLength := metainfo.ChoosePieceLength(tf.totalFileSizeBytes)

	private := true
	info, buildErr := tf.buildFromPathList(metainfo.Info{
		Private:     &private,
		PieceLength: pieceLength,
	})

	if buildErr != nil {
		return fmt.Errorf("error building torrent: %s", buildErr)
	}

	slices.Sort(info.Files, func(l, r metainfo.FileInfo) bool {
		return strings.Join(l.Path, "/") < strings.Join(r.Path, "/")
	})

	if info.PieceLength == 0 {
		info.PieceLength = metainfo.ChoosePieceLength(info.TotalLength())
	}

	if info.PieceLength == 0 {
		return errors.New("piece length must be non-zero")
	}

	pr, pw := io.Pipe()
	go func() {
		err := writeFiles(tf.root, &info, pw, tf.logOutput)
		pw.CloseWithError(err)
	}()
	defer pr.Close()

	var genErr error
	info.Pieces, genErr = generatePieces(pr, info.PieceLength, nil)
	if genErr != nil {
		return fmt.Errorf("error generating pieces: %s", genErr)
	}

	if genErr != nil {
		return fmt.Errorf("error generating pieces: %s", genErr)
	}

	var bencodeErr error
	tf.mi.InfoBytes, bencodeErr = bencode.Marshal(info)
	if bencodeErr != nil {
		return fmt.Errorf("errror bencoding info: %s", bencodeErr)
	}

	f, openErr := os.OpenFile(outFile, os.O_WRONLY|os.O_CREATE|os.O_CREATE, 0600)
	if openErr != nil {
		return fmt.Errorf("error opening file: %s", openErr)
	}
	defer f.Close()

	outErr := tf.mi.Write(f)
	if outErr != nil {
		return fmt.Errorf("error writing torrent file: %s", outErr)
	}

	endTime := time.Now()
	diff := endTime.Sub(startTime)
	if tf.logOutput {
		fmt.Println("Torrent created in", diff.Seconds(), "seconds")
	}

	return nil

}

// MagnetURL returns the magnet url for the torrent
func (tf *torrentFile) MagnetURL() string {
	return tf.mi.Magnet(nil, nil).String()
}

// New creates a new TorrentFile
func New(root, comment string, announce []string, logOutput bool) (TorrentFile, error) {

	mi := metainfo.MetaInfo{
		AnnounceList: [][]string{},
		Comment:      comment,
		CreatedBy:    "github.com/concretelabs/milkdud",
		CreationDate: time.Now().Unix(),
	}

	for _, tracker := range announce {
		mi.AnnounceList = append(mi.AnnounceList, []string{tracker})
	}

	tf := torrentFile{
		mi:        &mi,
		paths:     map[string]int64{},
		files:     []metainfo.FileInfo{},
		root:      root,
		announce:  announce,
		logOutput: logOutput,
	}

	return &tf, nil
}

// writeFiles writes the files in info to as fast as possible
func writeFiles(root string, info *metainfo.Info, w io.Writer, logOutput bool) error {

	files := info.UpvertedFiles()
	c := make(chan metainfo.FileInfo)
	results := make(chan string)

	worker := func(i int, wg *sync.WaitGroup) error {
		g := new(errgroup.Group)

		g.Go(func() error {
			for fi := range c {
				p := filepath.Join(root, strings.Join(fi.Path, string(filepath.Separator)))

				f, err := os.Open(p)
				if err != nil {
					return fmt.Errorf("error opening %v: %s", fi, err)
				}

				wn, err := io.CopyN(w, f, fi.Length)
				f.Close()

				if wn != fi.Length {
					return fmt.Errorf("error copying %v: %s", fi, err)
				}

				results <- p
			}
			wg.Done()
			return nil
		})

		if err := g.Wait(); err != nil {
			return err
		}

		return nil
	}

	createWorkerPool := func(workerCnt int) {
		var wg sync.WaitGroup
		for i := 0; i < workerCnt; i++ {
			wg.Add(1)
			worker(i, &wg)
		}
		wg.Wait()
		close(results)
	}

	result := func(logOutput bool, done chan bool) {
		for range results {
			if logOutput {
				fmt.Printf(".")
			}
		}
		if logOutput {
			fmt.Printf("\n")
		}
		done <- true
	}

	// allocate
	go func() {
		//  allocate
		for _, fi := range files {
			c <- fi
		}
		close(c)
	}()

	done := make(chan bool)
	go result(logOutput, done)

	workers := runtime.NumCPU()
	createWorkerPool(workers)

	<-done

	return nil
}

// generatePieces generates the pieces for the torrent
func generatePieces(r io.Reader, pieceLength int64, b []byte) ([]byte, error) {
	for {
		h := sha1.New()
		written, err := io.CopyN(h, r, pieceLength)
		if written > 0 {
			b = h.Sum(b)
		}
		if err == io.EOF {
			return b, nil
		}
		if err != nil {
			return b, err
		}
	}
}
