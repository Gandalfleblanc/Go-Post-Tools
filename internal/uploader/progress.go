package uploader

import (
	"io"
	"math"
	"time"
)

type UploadProgress struct {
	Percent float64
	SpeedMB float64
}

// progressReader wraps the pipe reader (côté HTTP client) pour mesurer
// les octets réellement lus par le client HTTP — pas la vitesse disque.
type progressReader struct {
	r          io.Reader
	total      int64
	read       int64
	start      time.Time
	lastEmit   time.Time
	onProgress func(UploadProgress)
}

func newProgressReader(r io.Reader, total int64, cb func(UploadProgress)) *progressReader {
	now := time.Now()
	return &progressReader{r: r, total: total, start: now, lastEmit: now, onProgress: cb}
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.r.Read(p)
	if n > 0 && pr.onProgress != nil {
		pr.read += int64(n)
		// Throttle: émet au max tous les 250 ms pour lisser la barre
		if time.Since(pr.lastEmit) < 250*time.Millisecond {
			return
		}
		pr.lastEmit = time.Now()
		elapsed := time.Since(pr.start).Seconds()
		speed := 0.0
		if elapsed > 0.1 {
			speed = float64(pr.read) / elapsed / 1024 / 1024
		}
		pct := float64(pr.read) / float64(pr.total) * 100
		pr.onProgress(UploadProgress{
			Percent: math.Min(pct, 99), // cap à 99% jusqu'à réponse serveur
			SpeedMB: speed,
		})
	}
	return
}
