package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"concretelabs/milkdud/beets"
)

const (

	// maxDepth is the maximum number of directories to scan before skipping the rest
	maxDepth = 32

	// default trackers via https://raw.githubusercontent.com/ngosang/trackerslist/master/trackers_best.txt
	defaultAnnounce = "udp://open.stealth.si:80/announce,udp://tracker.opentrackr.org:1337/announce,udp://tracker.openbittorrent.com:6969/announce"

	// root folder for all files in the torrent
	torrentFsRoot = "music"
)

var (
	// regular expression used to extract the TOCID from an Accurip log
	tocIDRegexp = regexp.MustCompile(`.*\[CTDB\sTOCID:\s(.*)\]\sfound.*`)
)

var (
	flagJsonOutput    = flag.Bool("j", false, "json stats")
	flagCreateTorrent = flag.Bool("t", false, "create torrent")
	flagTorrentName   = flag.String("n", "milkdud", "torrent filename")
	flagIgnoreRipLogs = flag.Bool("r", false, "ignore rip logs")
	flagImportArt     = flag.Bool("i", false, "include album art (jpeg image files) in torrent file")
	flagAnnounce      = flag.String("a", defaultAnnounce, "comma seperated announce URL(s)")
	FlagBeetsDBPath   = flag.String("b", "", "path to beets database file ex: musiclibrary.db")
	FlagDetailedStats = flag.Bool("d", false, "show detailed stats")
	FlagTorrentTag    = flag.String("g", "", "comma seperated tags for torrent comment ex: foo,bar")
)

type Stats struct {
	Path                  string `json:"path"`
	FolderCnt             int64  `json:"folder_count"`
	AccuripFolderCnt      int64  `json:"accurip_folder_count"`
	FoldersScanned        int64  `json:"folders_scanned"`
	TotalFileSize         string `json:"total_file_size"`
	TotalFileSizeBytes    int64  `json:"total_file_size_bytes"`
	TotalFiles            int64  `json:"total_files"`
	TotalFlacFiles        int64  `json:"total_flac_files"`
	AverageAlbumSize      string `json:"average_album_size"`
	AverageAlbumSizeBytes int64  `json:"average_album_size_bytes"`
	MagnetURL             string `json:"magnet_url,omitempty"`
	TorrentFileName       string `json:"torrent_file_name,omitempty"`
	Errors                int    `json:"errors"`
}

type DetailedStats struct {
	Stats
	Albums         []MusicFolder `json:"albums"`
	SkippedFolders []string      `json:"skipped_folders"`
	Errors         []error       `json:"errors"`
}

type scanResult struct {
	folder *MusicFolder
	err    error
}

type fileData struct {
	path string
	name string
	size int64
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

	scanResults := make(chan scanResult)

	// try and use beets
	if len(*FlagBeetsDBPath) > 0 {
		if !*flagJsonOutput {
			fmt.Println("Using Beets database file", *FlagBeetsDBPath)
		}

		// crawl the beets database
		go func() {
			crawlErr := crawlBeetsDB(*FlagBeetsDBPath, scanResults)
			if crawlErr != nil {
				fmt.Println(crawlErr)
				os.Exit(1)
			}
			close(scanResults)
		}()

		// otherwise scan the filesystem
	} else {
		if !*flagJsonOutput {
			fmt.Println("Beets database not specified, scanning", scanPath)
		}

		go func() {
			walkErr := crawlFs(scanPath, scanResults)
			if walkErr != nil {
				fmt.Println(walkErr)
				os.Exit(1)
			}
			close(scanResults)
		}()

	}

	// stats stores the results of the scan
	stats := Stats{
		Path:             scanPath,
		TotalFileSize:    "0 MB",
		AverageAlbumSize: "0 MB",
		MagnetURL:        "",
		TorrentFileName:  "",
	}

	albums := []MusicFolder{}
	skippedFolders := []string{}
	errors := []error{}
	fd := []fileData{}

	// loop through the music folders discovered
	for result := range scanResults {
		if result.err != nil {
			stats.Errors = stats.Errors + 1
			errors = append(errors, result.err)
			if !*flagJsonOutput {
				fmt.Printf("x")
			}
			continue
		} else {
			if !*flagJsonOutput {
				fmt.Printf(".")
			}
		}

		folder := result.folder
		stats.FoldersScanned = stats.FoldersScanned + 1

		// we ignore any folders that don't have an accurip log
		if folder.HasAccurip || *flagIgnoreRipLogs {
			if folder.HasAccurip {
				stats.AccuripFolderCnt = stats.AccuripFolderCnt + 1
			}
			stats.FolderCnt = stats.FolderCnt + 1
			stats.TotalFileSizeBytes = stats.TotalFileSizeBytes + folder.TotalBytes
			stats.TotalFileSize = byteCountSI(stats.TotalFileSizeBytes)
			stats.TotalFiles = stats.TotalFiles + folder.FileCnt
			stats.AverageAlbumSizeBytes = stats.TotalFileSizeBytes / stats.FolderCnt
			stats.AverageAlbumSize = byteCountSI(stats.AverageAlbumSizeBytes)
			albums = append(albums, *folder)

			for _, file := range folder.Files {
				if file.FileType == FileTypeFlac {
					stats.TotalFlacFiles = stats.TotalFlacFiles + 1
				}
				fd = append(fd, fileData{folder.Path, file.Name, file.Size})
			}

		} else {
			if folder.Path != scanPath {
				skippedFolders = append(skippedFolders, folder.Path)
			}
		}
	}

	if !*flagJsonOutput {
		fmt.Printf("\n")
	}

	detailedStats := DetailedStats{
		stats,
		albums,
		skippedFolders,
		errors,
	}

	// summarize the album size results
	if !*flagJsonOutput {
		fmt.Println("Completed successfully")
		fmt.Println("Folders:", stats.FoldersScanned)
		fmt.Println("Folders with Accurip logs:", stats.AccuripFolderCnt)
		fmt.Println("Files:", stats.TotalFiles)
		fmt.Println("Flac files:", stats.TotalFlacFiles)
		fmt.Println("Total file size:", stats.TotalFileSize, fmt.Sprintf("(%d bytes)", stats.TotalFileSizeBytes))
		fmt.Println("Average album size:", stats.AverageAlbumSize, fmt.Sprintf("(%d bytes)", stats.AverageAlbumSizeBytes))
		if len(detailedStats.Errors) > 0 {
			fmt.Println("Errors:")
			for _, err := range detailedStats.Errors {
				fmt.Println(" ", err)
			}
		}
		if *FlagDetailedStats {
			fmt.Println("Scanned albums:")
			for _, mf := range detailedStats.Albums {
				fmt.Println(" ", mf.Path, mf.HasAccurip, mf.FlacCnt, mf.FileCnt, byteCountSI(mf.TotalBytes))
				for _, file := range mf.Files {
					fmt.Println("  ", file.Name)
				}
			}
		}
	}

	// create torrent file for all album files
	if *flagCreateTorrent {
		if stats.TotalFileSizeBytes == 0 {
			if !*flagJsonOutput {
				fmt.Println("No files, skipping torrent creation")
			}
		} else {

			stats.TorrentFileName = fmt.Sprintf("%s.torrent", *flagTorrentName)
			magnetURL, torrentErr := createTorrent(torrentFsRoot, scanPath, stats.TorrentFileName, stats.AccuripFolderCnt, fd)
			if torrentErr != nil {
				fmt.Println(torrentErr)
				os.Exit(1)
			}
			stats.MagnetURL = magnetURL

			if !*flagJsonOutput {
				fmt.Println("Magnet URL:", stats.MagnetURL)
				fmt.Println("Torrent created:", stats.TorrentFileName)
			}
		}

	}

	if *flagJsonOutput {
		var b []byte

		if *FlagDetailedStats {
			b, _ = json.MarshalIndent(detailedStats, "", "  ")
		} else {
			b, _ = json.MarshalIndent(stats, "", "  ")
		}

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

// crawlFolder crawls a folder for flac files and accurip logs
func crawlFolder(dir string) (*MusicFolder, error) {
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

			switch FileType(ext) {
			case FileTypeFlac:
				mf.TotalBytes = mf.TotalBytes + info.Size()
				mf.FileCnt = mf.FileCnt + 1
				mf.FlacCnt = mf.FlacCnt + 1
				mf.Files = append(mf.Files, MusicFile{
					Path:     p,
					Name:     info.Name(),
					Size:     info.Size(),
					FileType: FileTypeFlac,
				})

			case FileTypeAccurip:
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

			case FileTypeLog:
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

			case FileTypeJpg:
				fallthrough

			case FileTypeJpeg:
				if *flagImportArt {
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

// crawlBeetsDB crawls folders based on albums from the beets database
func crawlBeetsDB(beetsDB string, scanResults chan<- scanResult) error {
	bdb, beetsErr := beets.New(beetsDB)
	if beetsErr != nil {
		return beetsErr
	}

	albums, albumsErr := bdb.GetAllAlbums()
	if albumsErr != nil {
		return albumsErr
	}

	if len(albums) == 0 {
		return fmt.Errorf("no albums found in beets database")
	}

	for _, album := range albums {
		album, albumErr := bdb.GetAlbum(album.ID)
		if albumErr != nil {
			panic(albumErr)
		}

		mf, crawlErr := crawlFolder(album.Path)
		scanResults <- scanResult{
			mf,
			crawlErr,
		}

	}

	return nil
}

// crawlFs crawls folders based on albums from the supplied path
func crawlFs(scanPath string, scanResults chan<- scanResult) error {

	_, err := os.Stat(scanPath)
	if os.IsNotExist(err) || len(scanPath) == 0 {
		return err
	}

	return filepath.WalkDir(scanPath, func(p string, di fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// skip the rest of the path if we've exceeded the max depth
		if di.IsDir() && strings.Count(p, string(os.PathSeparator)) > maxDepth {
			fmt.Println("skipping", p, ", exceeded max depth of", maxDepth, "directories")
			return fs.SkipDir
		}

		if di.IsDir() && p != scanPath {
			mf, crawlErr := crawlFolder(p)
			scanResults <- scanResult{
				mf,
				crawlErr,
			}
		}

		return nil
	})
}

// createTorrent creates a torrent file
func createTorrent(fsRoot, scanPath, fileName string, accuruipFolderCnt int64, fd []fileData) (string, error) {
	if !*flagJsonOutput {
		fmt.Println("Creating torrent file. Please be patient, it may take a while...")
	}

	trackerlist := strings.Split(*flagAnnounce, ",")

	// create torrent file
	tf, err := createTorrentFile(scanPath, trackerlist)
	if err != nil {
		return "", err
	}

	for _, f := range fd {
		itemPath := filepath.Join(f.path, f.name)
		tf.AddFile(itemPath, f.size)
	}

	comment := fmt.Sprintf("This torrent was created by milkdud. Contains %d Accurip albums.", accuruipFolderCnt)
	if len(*FlagTorrentTag) > 0 {
		comment = fmt.Sprintf("%s (%s)", comment, *FlagTorrentTag)
	}

	magnetURL, torrentErr := tf.Create(fsRoot, fileName, comment)
	if torrentErr != nil {
		return "", torrentErr
	}

	return magnetURL, nil
}
