package torrent

import (
	"fmt"
	"os"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
)

type Progress struct {
	Percent float64 `json:"percent"`
	Stage   string  `json:"stage"` // "hashing" | "writing"
}

// Create génère un .torrent à partir d'un fichier source.
// trackerURL peut être vide (torrent "trackerless").
// pieceSize en octets (défaut conseillé: 8 MiB).
func Create(filePath, trackerURL, outputPath string, pieceSize int64, onProgress func(Progress)) error {
	if pieceSize <= 0 {
		pieceSize = 8 * 1024 * 1024
	}

	private := true
	info := metainfo.Info{
		PieceLength: pieceSize,
		Private:     &private,
	}
	if err := info.BuildFromFilePath(filePath); err != nil {
		return fmt.Errorf("hashing %s: %w", filePath, err)
	}

	if onProgress != nil {
		onProgress(Progress{Percent: 99, Stage: "hashing"})
	}

	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		return fmt.Errorf("marshal info: %w", err)
	}

	mi := metainfo.MetaInfo{
		InfoBytes: infoBytes,
	}
	if trackerURL != "" {
		mi.Announce = trackerURL
		mi.AnnounceList = [][]string{{trackerURL}}
	}

	f, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("create %s: %w", outputPath, err)
	}
	defer f.Close()

	if err := mi.Write(f); err != nil {
		return fmt.Errorf("write torrent: %w", err)
	}

	if onProgress != nil {
		onProgress(Progress{Percent: 100, Stage: "writing"})
	}
	return nil
}
