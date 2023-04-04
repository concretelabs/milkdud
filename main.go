package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"concretelabs/milkdud/beets"
)

const (
	// cueToolsLookupURL is the URL to the CueTools database lookup page
	cueToolsLookupURL = "http://db.cuetools.net/top.php?tocid=%s"

	// maxDepth is the maximum number of directories to scan before skipping the rest
	maxDepth = 32

	// default trackers via https://raw.githubusercontent.com/ngosang/trackerslist/master/trackers_best.txt
	defaultAnnounce = "udp://open.stealth.si:80/announce,udp://tracker.opentrackr.org:1337/announce,udp://tracker.openbittorrent.com:6969/announce"
)

var (
	// regular expression used to extract the TOCID from an Accurip log
	tocIDRegexp = regexp.MustCompile(`.*\[CTDB\sTOCID:\s(.*)\]\sfound.*`)
)

var (
	jsonOutputPtr    = flag.Bool("j", false, "json output")
	createTorrentPtr = flag.Bool("t", false, "create torrent")
	torrentNamePtr   = flag.String("n", "milkdud", "torrent filename")
	ignoreRipLogsPtr = flag.Bool("r", false, "ignore rip logs")
	importArtPtr     = flag.Bool("i", false, "include album art (jpeg image files) in torrent file")
	announcePtr      = flag.String("a", defaultAnnounce, "comma seperated announce URL(s)")
	beetsDBPathPtr   = flag.String("b", "", "path to beets database file ex: musiclibrary.db")
)

// Output is the struct for the json output
type Output struct {
	Path                  string        `json:"path"`
	FolderCnt             int64         `json:"folder_count"`
	AccuripFolderCnt      int64         `json:"accurip_folder_count"`
	FoldersScanned        int64         `json:"folders_scanned"`
	TotalFileSize         string        `json:"total_file_size"`
	TotalFileSizeBytes    int64         `json:"total_file_size_bytes"`
	TotalFiles            int64         `json:"total_files"`
	AverageAlbumSize      string        `json:"average_album_size"`
	AverageAlbumSizeBytes int64         `json:"average_album_size_bytes"`
	Albums                []MusicFolder `json:"albums"`
	SkippedFolders        []string      `json:"skipped_folders"`
	MagnetURL             string        `json:"magnet_url,omitempty"`
	TorrentFileName       string        `json:"torrent_file_name,omitempty"`
	torrentFileList       map[string]int64
}

func main() {
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		flag.PrintDefaults()
	}

	flag.Parse()

	if len(os.Args) == 1 {
		flag.Usage()
		os.Exit(1)
	}

	// path should be the last argument
	scanPath := os.Args[len(os.Args)-1]

	folders := []MusicFolder{}

	// try and use beets
	if len(*beetsDBPathPtr) > 0 {
		if !*jsonOutputPtr {
			fmt.Println("Using Beets database file", *beetsDBPathPtr)
		}

		var crawlErr error
		folders, crawlErr = CrawlBeetsDB(*beetsDBPathPtr)
		if crawlErr != nil {
			fmt.Println(crawlErr)
			os.Exit(1)
		}

		for folder := range folders {
			fmt.Println("folder:", folder)
		}

	} else {
		if !*jsonOutputPtr {
			fmt.Println("Beets database not specified, scanning", scanPath)
		}

		_, err := os.Stat(scanPath)
		if os.IsNotExist(err) || len(scanPath) == 0 {
			flag.Usage()
			fmt.Println("")
			fmt.Println("invalid path")
			os.Exit(1)
		}

		// walk the filesystem
		walkErr := filepath.WalkDir(scanPath, func(p string, di fs.DirEntry, err error) error {

			if err != nil {
				return err
			}

			// skip the rest of the path if we've exceeded the max depth
			if di.IsDir() && strings.Count(p, string(os.PathSeparator)) > maxDepth {
				fmt.Println("skipping", p, ", exceeded max depth of", maxDepth, "directories")
				return fs.SkipDir
			}

			if di.IsDir() && p != scanPath {

				mf, crawlErr := CrawlFolder(p)
				if crawlErr != nil {
					return fmt.Errorf("error crawling folder: %s", crawlErr)
				}

				folders = append(folders, *mf)
				log.Println("crawled", mf.Path)
			}

			return nil
		})

		if walkErr != nil {
			fmt.Println(walkErr)
			os.Exit(1)
		}

	}

	// output stores the results of the scan
	output := Output{
		Path:                  scanPath,
		FolderCnt:             0,
		AccuripFolderCnt:      0,
		FoldersScanned:        0,
		TotalFileSize:         "0 MB",
		TotalFileSizeBytes:    0,
		TotalFiles:            0,
		AverageAlbumSize:      "0 MB",
		AverageAlbumSizeBytes: 0,
		Albums:                []MusicFolder{},
		SkippedFolders:        []string{},
		MagnetURL:             "",
		TorrentFileName:       "",
		torrentFileList:       map[string]int64{},
	}

	trackerlist := strings.Split(*announcePtr, ",")

	// create torrent file
	tf, tfErr := createTorrentFile(scanPath, trackerlist)
	if tfErr != nil {
		fmt.Println(tfErr)
		os.Exit(1)
	}

	if !*jsonOutputPtr {
		fmt.Println("Scanning", scanPath, "for flac files with Accurip logs...")
	}

	// loop through the music folders discovered
	for _, folder := range folders {

		accuripFound := false
		if len(folder.TocID) > 0 {
			accuripFound = true
		}

		if !*jsonOutputPtr {
			if accuripFound {
				fmt.Println(folder.Path, "-", folder.FlacCnt, "flac files,", folder.FileCnt, "total,", byteCountSI(folder.TotalBytes), " Accurip confirmed! TOC ID:", folder.TocID)
			} else {
				fmt.Println(folder.Path, "-", folder.FlacCnt, "flac files,", folder.FileCnt, "total,", byteCountSI(folder.TotalBytes), " no Accurip log found")
			}
		}

		output.FoldersScanned = output.FoldersScanned + 1

		// we ignore any folders that don't have an accurip log
		if accuripFound || *ignoreRipLogsPtr {
			if accuripFound {
				output.AccuripFolderCnt = output.AccuripFolderCnt + 1
			}
			output.FolderCnt = output.FolderCnt + 1
			output.TotalFileSizeBytes = output.TotalFileSizeBytes + folder.TotalBytes
			output.TotalFileSize = byteCountSI(output.TotalFileSizeBytes)
			output.TotalFiles = output.TotalFiles + folder.FileCnt
			output.AverageAlbumSizeBytes = output.AverageAlbumSizeBytes + folder.TotalBytes
			output.AverageAlbumSize = byteCountSI(output.AverageAlbumSizeBytes / output.FolderCnt)
			output.Albums = append(output.Albums, folder)

			for _, file := range folder.Files {
				itemPath := filepath.Join(folder.Path, file.Name)
				output.torrentFileList[itemPath] = file.Size
				tf.AddFile(itemPath, file.Size)
			}

		} else {
			if folder.Path != scanPath {
				output.SkippedFolders = append(output.SkippedFolders, folder.Path)
			}
		}
	}

	// summarize the album size results
	if !*jsonOutputPtr {
		fmt.Println("Folders scanned:", output.FoldersScanned)
		fmt.Println("Folders with Accurip logs found:", output.AccuripFolderCnt)
		fmt.Println("Number of files:", output.TotalFiles)
		fmt.Println("Total file size:", output.TotalFileSize, fmt.Sprintf("(%d bytes)", output.TotalFileSizeBytes))
		fmt.Println("Average album size:", output.AverageAlbumSize)
	}

	// create torrent file for all album files
	if *createTorrentPtr {
		output.TorrentFileName = fmt.Sprintf("%s.torrent", *torrentNamePtr)

		if !*jsonOutputPtr {
			fmt.Println("Creating torrent file. Please be patient, it may take a while...")
		}

		// fancy comment
		comment := fmt.Sprintf("This torrent was created by milkdud. %d Accurip logs found", output.AccuripFolderCnt)

		magnetURL, torrentErr := tf.Create(output.TorrentFileName, comment)
		if torrentErr != nil {
			fmt.Println(torrentErr)
			os.Exit(1)
		}
		output.MagnetURL = magnetURL

		if !*jsonOutputPtr {
			fmt.Println("Magnet URL:", magnetURL)
			fmt.Println("Torrent created:", output.TorrentFileName)
		}

	}

	if *jsonOutputPtr {
		b, _ := json.MarshalIndent(output, "", "  ")
		fmt.Println(string(b))
		os.Exit(0)
	}
}

// byteCountSI returns a human readable byte count
// via https://yourbasic.org/golang/formatting-byte-size-to-human-readable-format/
func byteCountSI(b int64) string {
	const unit = 1000
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB",
		float64(b)/float64(div), "kMGTPE"[exp])
}

// detectAccuripInFile detects the TOCID in an Accurip log file
func detectAccuripInFile(logFile string) (string, error) {
	contents, readErr := os.ReadFile(logFile)
	if readErr != nil {
		return "", readErr
	}

	tocIdFromEAC, eacErr := detectEACTOCID(string(contents))
	if eacErr != nil {
		return "", eacErr
	}
	if len(tocIdFromEAC) > 0 {
		return tocIdFromEAC, nil
	}

	tocIdFromCueRipper, cueErr := detectCUERipperTOCID(string(contents))
	if cueErr != nil {
		return "", cueErr
	}
	if len(tocIdFromCueRipper) > 0 {
		return tocIdFromCueRipper, nil
	}

	return "", nil
}

// detectAccuripInFile detects the TOCID in an Accurip log file generated by EAC
func detectEACTOCID(str string) (string, error) {

	// remove \x00 runes (NULL) as EAC tends to put these in the log file
	str = strings.Replace(str, "\x00", "", -1)

	if strings.Contains(str, "Exact Audio Copy") {
		if strings.Contains(str, "has been confirmed") {
			matches := tocIDRegexp.FindStringSubmatch(str)
			if len(matches) > 0 {
				tocID := tocIDRegexp.FindStringSubmatch(str)[1]
				return tocID, nil
			}
		}
	}

	return "", nil
}

// detectAccuripInFile detects the TOCID in an Accurip log file generated by CUETools
func detectCUERipperTOCID(str string) (string, error) {

	if strings.Contains(str, "CUETools log") {
		matches := tocIDRegexp.FindStringSubmatch(str)
		if len(matches) > 0 {
			tocID := tocIDRegexp.FindStringSubmatch(str)[1]
			return tocID, nil
		}
	}

	return "", nil
}

// CrawlFolder crawls a folder for flac files and accurip logs
func CrawlFolder(dir string) (*MusicFolder, error) {
	if len(dir) == 0 {
		return nil, fmt.Errorf("no directory specified")
	}

	// Check if the directory exists
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("directory does not exist: %s", dir)
		} else {
			return nil, fmt.Errorf("error reading directory: %s", dir)
		}
	}

	if !info.IsDir() {
		return nil, fmt.Errorf("the provided path is not a directory: %s", dir)
	}

	mf := MusicFolder{
		Path:       dir,
		HasAccurip: false,
		TocID:      "",
		Files:      []MusicFile{},
		FileCnt:    0,
		FlacCnt:    0,
		TotalBytes: 0,
	}

	// loop through the files in the directory
	walkErr := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("error walking directory: %s", err)
		}

		if !d.IsDir() {

			ext := strings.Replace(path.Ext(d.Name()), ".", "", -1)
			info, infoErr := d.Info()
			if infoErr != nil {
				panic(infoErr)
			}

			switch ext {
			case "flac":
				mf.TotalBytes = mf.TotalBytes + info.Size()
				mf.FileCnt = mf.FileCnt + 1
				mf.FlacCnt = mf.FlacCnt + 1
				mf.Files = append(mf.Files, MusicFile{
					Path:     p,
					Name:     info.Name(),
					Size:     info.Size(),
					FileType: FileTypeFlac,
				})

			case "accurip":
				id, accuripErr := detectAccuripInFile(p)
				if accuripErr != nil {
					return fmt.Errorf("error reading accurip log file %s: %s", d.Name(), accuripErr)
				} else {
					if len(id) > 0 {
						mf.HasAccurip = true
						mf.TocID = id
						mf.TotalBytes = mf.TotalBytes + info.Size()
						mf.FileCnt = mf.FileCnt + 1
						mf.Files = append(mf.Files, MusicFile{
							Path:     p,
							Name:     info.Name(),
							Size:     info.Size(),
							FileType: FileTypeAccurip,
						})
					}
				}

			case "log":
				id, accuripErr := detectAccuripInFile(p)
				if accuripErr != nil {
					return fmt.Errorf("error reading accurip log file %s: %s", d.Name(), accuripErr)
				} else {
					if len(id) > 0 {
						mf.HasAccurip = true
						mf.TocID = id
						mf.TotalBytes = mf.TotalBytes + info.Size()
						mf.FileCnt = mf.FileCnt + 1
						mf.Files = append(mf.Files, MusicFile{
							Path:     p,
							Name:     info.Name(),
							Size:     info.Size(),
							FileType: FileTypeLog,
						})
					}
				}

			case "jpg":
				if *importArtPtr {
					mf.TotalBytes = mf.TotalBytes + info.Size()
					mf.FileCnt = mf.FileCnt + 1
					mf.Files = append(mf.Files, MusicFile{
						Path:     p,
						Name:     info.Name(),
						Size:     info.Size(),
						FileType: FileTypeJpg,
					})
				}

			case "jpeg":
				if *importArtPtr {
					mf.TotalBytes = mf.TotalBytes + info.Size()
					mf.FileCnt = mf.FileCnt + 1
					mf.Files = append(mf.Files, MusicFile{
						Path:     p,
						Name:     info.Name(),
						Size:     info.Size(),
						FileType: FileTypeJpeg,
					})
				}

			default:
				// log.Println("ignoring file:", d.Name())
			}
		}

		return nil
	})

	if walkErr != nil {
		return nil, fmt.Errorf("error walking directory: %s", walkErr)
	}

	return &mf, nil
}

// CrawlBeetsDB crawls folders based on albums from the beets database
func CrawlBeetsDB(beetsDB string) ([]MusicFolder, error) {
	folders := []MusicFolder{}

	bdb, beetsErr := beets.New(beetsDB)
	if beetsErr != nil {
		return nil, beetsErr
	}

	albums, albumsErr := bdb.GetAllAlbums()
	if albumsErr != nil {
		return nil, albumsErr
	}

	if len(albums) == 0 {
		return nil, fmt.Errorf("no albums found in beets database")
	}

	for _, album := range albums {
		album, albumErr := bdb.GetAlbum(album.ID)
		if albumErr != nil {
			panic(albumErr)
		}

		mf, crawlErr := CrawlFolder(album.Path)
		if crawlErr != nil {
			if !*jsonOutputPtr {
				fmt.Printf("error crawling folder %s", crawlErr)
			}
			continue
		}

		folders = append(folders, *mf)
	}

	return folders, nil

}
