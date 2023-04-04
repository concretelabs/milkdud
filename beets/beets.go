package beets

import (
	"database/sql"
	"fmt"
	"log"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// Track represents a single track in an album
type Track struct {
	ID   int    `json:"id"`
	Path string `json:"path"`
}

// AlbumSummary represents a summary of an album
type AlbumSummary struct {
	ID     int    `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
}

// Album represents an album with tracks
type Album struct {
	ID        int     `json:"id"`
	Path      string  `json:"path"`
	Title     string  `json:"title"`
	Artist    string  `json:"artist"`
	ArtistID  string  `json:"mb_artist_id"` // MusicBrainz ID
	AlbumID   string  `json:"album_id"`     // MusicBrainz ID
	ItemCount int     `json:"item_count"`
	Tracks    []Track `json:"tracks"`
}

// item represents a single item in the beets database
type item struct {
	ID       int
	Path     string
	Title    string
	Artist   string
	ArtistID string
	AlbumID  string
}

// Beets interface for beets database access
type Beets interface {
	GetAllAlbums() ([]AlbumSummary, error)
	GetAlbum(albumID int) (*Album, error)
	PrintTableInfo(tableName string)
}

// beets is the implementation of the Beets interface
type beets struct {
	dbFile string
	db     *sql.DB
}

// PrintTableInfo prints the table info for the given table name
func (b *beets) PrintTableInfo(tableName string) {
	rows, err := b.db.Query(fmt.Sprintf("PRAGMA table_info(%s)", tableName))
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	fmt.Println("Column | Type | Not Null | Default Value | Primary Key")
	for rows.Next() {
		var cid, notnull, pk int
		var name, ctype string
		var dflt_value sql.NullString
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt_value, &pk); err != nil {
			log.Fatal(err)
		}
		defaultValue := "NULL"
		if dflt_value.Valid {
			defaultValue = dflt_value.String
		}
		fmt.Printf("%s | %s | %d | %s | %d\n", name, ctype, notnull, defaultValue, pk)
	}

	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}
}

// GetAlbums reads the albums from the beets database
func (b *beets) GetAllAlbums() ([]AlbumSummary, error) {

	albums := []AlbumSummary{}

	rows, err := b.db.Query(`SELECT id, albumartist, album FROM albums`)
	if err != nil {
		return nil, fmt.Errorf("error querying albums from beets database %s", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var albumartist, album string
		if err := rows.Scan(&id, &albumartist, &album); err != nil {
			return nil, fmt.Errorf("error scanning rows in beets database %s", err)
		}

		albums = append(albums, AlbumSummary{
			ID:     id,
			Title:  album,
			Artist: albumartist,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading albums from beets database %s", err)
	}

	return albums, nil
}

// GetAlbum reads a complete set of album data from the beets database
func (b *beets) GetAlbum(albumID int) (*Album, error) {

	album := Album{
		ID:        albumID,
		Path:      "",
		Artist:    "",
		Title:     "",
		ArtistID:  "",
		AlbumID:   "",
		ItemCount: 0,
		Tracks:    []Track{},
	}

	tracks, tracksErr := b.getAlbumTracks(albumID)
	if tracksErr != nil {
		return nil, fmt.Errorf("failed to get album items %s", tracksErr)
	}

	if len(tracks) == 0 {
		return nil, fmt.Errorf("album had no items %s", tracksErr)
	}

	for _, track := range tracks {
		album.Tracks = append(album.Tracks, Track{
			ID:   track.ID,
			Path: track.Path,
		})

		if album.ItemCount == 0 {
			album.Path = filepath.Dir(track.Path)
			album.Title = track.Title
			album.Artist = track.Artist
			album.ArtistID = track.ArtistID
			album.AlbumID = track.AlbumID
		}

		album.ItemCount = album.ItemCount + 1
	}

	return &album, nil
}

// getAlbumTracks reads album tracks (items) from the beets database
func (b *beets) getAlbumTracks(albumID int) ([]item, error) {

	items := []item{}

	rows, err := b.db.Query(fmt.Sprintf("SELECT id, path, album_id, title, artist, discogs_albumid, discogs_artistid, mb_trackid, mb_albumid, mb_artistid FROM items WHERE album_id = '%d'", albumID))
	if err != nil {
		return nil, fmt.Errorf("error querying items from beets database %s", err)
	}
	defer rows.Close()

	for rows.Next() {
		var id int
		var path string
		var album_id, title, artist, discogs_albumid, discogs_artistid, mb_trackid, mb_albumid, mb_artistid string
		if err := rows.Scan(&id, &path, &album_id, &title, &artist, &discogs_albumid, &discogs_artistid, &mb_trackid, &mb_albumid, &mb_artistid); err != nil {
			return nil, fmt.Errorf("error scanning rows in beets database %s", err)
		}

		items = append(items, item{
			ID:       id,
			Title:    title,
			Artist:   artist,
			ArtistID: mb_artistid,
			AlbumID:  mb_albumid,
			Path:     path,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error reading items from beets database %s", err)
	}

	return items, nil
}

// New creates a new beets instance that can be used to read the beets database
func New(dbFile string) (Beets, error) {
	if dbFile == "" {
		return nil, fmt.Errorf("beets musiclibrary file path is required")
	}

	db, err := sql.Open("sqlite3", dbFile)
	if err != nil {
		return nil, fmt.Errorf("error opening beets database %s", err)
	}

	return &beets{
		dbFile: dbFile,
		db:     db,
	}, nil
}
