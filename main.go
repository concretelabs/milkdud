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
)

// Album contains file data about a scanned album folder
type Album struct {
	FileCount       int64  `json:"file_count"`
	FlacCount       int64  `json:"flac_count"`
	TotalBytes      int64  `json:"total_bytes"`
	Path            string `json:"path"`
	TOCID           string `json:"tocid"`
	CueToolsDbURL   string `json:"cue_tools_lookup_url"`
	torrentFileList map[string]int64
}

// Output is the struct for the json output
type Output struct {
	Path                  string   `json:"path"`
	FolderCnt             int64    `json:"folder_count"`
	AccuripFolderCnt      int64    `json:"accurip_folder_count"`
	FoldersScanned        int64    `json:"folders_scanned"`
	TotalFileSize         string   `json:"total_file_size"`
	TotalFileSizeBytes    int64    `json:"total_file_size_bytes"`
	TotalFiles            int64    `json:"total_files"`
	AverageAlbumSize      string   `json:"average_album_size"`
	AverageAlbumSizeBytes int64    `json:"average_album_size_bytes"`
	Albums                []Album  `json:"albums"`
	SkippedFolders        []string `json:"skipped_folders"`
	MagnetURL             string   `json:"magnet_url,omitempty"`
	TorrentFileName       string   `json:"torrent_file_name,omitempty"`
	torrentFileList       map[string]int64
}

// scannedAlbums accepts all albums read by the scanner
var scannedAlbums chan Album

func main() {
	scannedAlbums = make(chan Album)

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

	_, err := os.Stat(scanPath)
	if os.IsNotExist(err) || len(scanPath) == 0 {
		flag.Usage()
		fmt.Println("")
		fmt.Println("invalid path")
		os.Exit(1)
	}

	// read the scanned albums into the channel
	go func() {
		err := filepath.WalkDir(scanPath, visit)
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		close(scannedAlbums)
	}()

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
		Albums:                []Album{},
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

	// read the scanned albums from the channel
	for album := range scannedAlbums {

		accuripFound := false
		if len(album.TOCID) > 0 {
			accuripFound = true
		}

		if !*jsonOutputPtr {
			if accuripFound {
				fmt.Println(album.Path, "-", album.FlacCount, "flac files,", album.FileCount, "total,", byteCountSI(album.TotalBytes), " Accurip confirmed! TOC ID:", album.TOCID)
			} else {
				fmt.Println(album.Path, "-", album.FlacCount, "flac files,", album.FileCount, "total,", byteCountSI(album.TotalBytes), " no Accurip log found")
			}
		}

		output.FoldersScanned = output.FoldersScanned + 1

		// we ignore any folders that don't have an accurip log
		if accuripFound || *ignoreRipLogsPtr {
			if accuripFound {
				output.AccuripFolderCnt = output.AccuripFolderCnt + 1
			}
			output.FolderCnt = output.FolderCnt + 1
			output.TotalFileSizeBytes = output.TotalFileSizeBytes + album.TotalBytes
			output.TotalFileSize = byteCountSI(output.TotalFileSizeBytes)
			output.TotalFiles = output.TotalFiles + album.FileCount
			output.AverageAlbumSizeBytes = output.AverageAlbumSizeBytes + album.TotalBytes
			output.AverageAlbumSize = byteCountSI(output.AverageAlbumSizeBytes / output.FolderCnt)
			output.Albums = append(output.Albums, album)

			for file, size := range album.torrentFileList {
				output.torrentFileList[file] = size
				tf.AddFile(file, size)
			}

		} else {
			if album.Path != scanPath {
				output.SkippedFolders = append(output.SkippedFolders, album.Path)
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

// visit is the callback function for filepath.WalkDir
func visit(p string, di fs.DirEntry, err error) error {

	if err != nil {
		return err
	}

	// skip the rest of the path if we've exceeded the max depth
	if di.IsDir() && strings.Count(p, string(os.PathSeparator)) > maxDepth {
		fmt.Println("skipping", p, ", exceeded max depth of", maxDepth, "directories")
		return fs.SkipDir
	}

	if di.IsDir() {
		torrentFileList := map[string]int64{}

		directoryFiles, dirErr := os.ReadDir(p)
		if dirErr != nil {
			return fmt.Errorf("error reading directory %s: %w", p, err)
		}

		fileCnt := int64(0)
		flacCnt := int64(0)
		totalBytes := int64(0)
		tocID := ""

		// loop through the files in the directory
		for _, directoryFile := range directoryFiles {

			ext := strings.Replace(path.Ext(directoryFile.Name()), ".", "", -1)
			info, infoErr := directoryFile.Info()
			if infoErr != nil {
				panic(infoErr)
			}

			switch ext {
			case "flac":
				totalBytes = totalBytes + info.Size()
				torrentFileList[path.Join(p, directoryFile.Name())] = info.Size()
				fileCnt = fileCnt + 1
				flacCnt = flacCnt + 1

			case "accurip":
				id, accuripErr := detectAccuripInFile(path.Join(p, directoryFile.Name()))
				if accuripErr != nil {
					fmt.Println("error reading accurip log file:", directoryFile.Name(), accuripErr)
				} else {
					if len(id) > 0 {
						tocID = id
						torrentFileList[path.Join(p, directoryFile.Name())] = info.Size()
						fileCnt = fileCnt + 1
					}
				}

			case "log":
				id, accuripErr := detectAccuripInFile(path.Join(p, directoryFile.Name()))
				if accuripErr != nil {
					fmt.Println("error reading accurip log file:", directoryFile.Name(), accuripErr)
				} else {
					if len(id) > 0 {
						tocID = id
						torrentFileList[path.Join(p, directoryFile.Name())] = info.Size()
						fileCnt = fileCnt + 1
					}
				}

			case "jpg":
				if *importArtPtr {
					torrentFileList[path.Join(p, directoryFile.Name())] = info.Size()
					fileCnt = fileCnt + 1
				}

			case "jpeg":
				if *importArtPtr {
					torrentFileList[path.Join(p, directoryFile.Name())] = info.Size()
					fileCnt = fileCnt + 1
				}

			default:
				// log.Println("ignoring file:", directoryFile.Name())
			}
		}

		url := ""
		if len(tocID) > 0 {
			url = fmt.Sprintf(cueToolsLookupURL, tocID)
		}

		scannedAlbums <- Album{
			FileCount:       fileCnt,
			FlacCount:       flacCnt,
			TotalBytes:      totalBytes,
			Path:            p,
			TOCID:           tocID,
			CueToolsDbURL:   url,
			torrentFileList: torrentFileList,
		}

	}

	return nil
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
