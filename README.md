# milkdud

**milkdud** is an opinionated tool for creating torrents from a large, well organized, and high quality music library.

Please seriously consider using Beet ([https://beets.io](https://beets.io)) to manage your music library *before* using this tool to generate a torrent. Having identical artist, album, and file names based on accuripped TOC ID's makes life better for everyone.

It is designed to work with a library that uses FLAC encoding with corresponding Accurip logs. All other formats like MP3 or WAV are ignored. Most of the common rip tools like [CUERipper](http://cue.tools/wiki/CUERipper) and [EAC](https://www.exactaudiocopy.de/) that generate a Accurip log file should be detectable by this tool.

This tool is intended for power users with large libraries who want to share.

## Usage

```
usage: milkdud [options] path
options:
  -a string
        comma seperated announce URL(s) (default "udp://open.stealth.si:80/announce,udp://tracker.opentrackr.org:1337/announce,udp://tracker.openbittorrent.com:6969/announce")
  -b string
        path to beets database file ex: musiclibrary.db
  -d    show detailed stats
  -i    include album art (jpeg image files) in torrent file
  -j    json stats
  -n string
        torrent filename (default "milkdud")
  -r    ignore rip logs
  -t    create torrent
```

Dry run example:
```
milkdud /path/to/music
```

Example usage:

This creates a torrent from a Beets DB:
```
milkdud -t -a http://yourtracker.com/announce/?id=secret -b musiclibrary.db
```

This creates a torrent without a Beets DB by scanning folders located in `/path/to/music`
```
milkdud -t http://yourtracker.com/announce/?id=secret /path/to/music
```

Notes:
* generating a torrent can take a very long time depending on how large your music library is and the speed of your hardware.
* all torrents are private by default

## Building

To build this locally:
```
git clone https://github.com/concretelabs/milkdud.git
cd milkdud
go build
```

<img src="milkduds.jpg" width="500px" />