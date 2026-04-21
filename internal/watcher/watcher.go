package watcher

import (
	"context"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Watcher surveille un dossier et émet les chemins de fichiers .mkv/.mp4 nouvellement stables.
type Watcher struct {
	mu        sync.Mutex
	fs        *fsnotify.Watcher
	cancel    context.CancelFunc
	folder    string
	onNewFile func(path string)
}

func New(folder string, onNewFile func(path string)) *Watcher {
	return &Watcher{folder: folder, onNewFile: onNewFile}
}

// Start lance la surveillance. Retourne une erreur si le dossier n'existe pas.
func (w *Watcher) Start() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fs != nil {
		return nil
	}
	fw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := fw.Add(w.folder); err != nil {
		fw.Close()
		return err
	}
	w.fs = fw
	ctx, cancel := context.WithCancel(context.Background())
	w.cancel = cancel
	go w.loop(ctx)
	return nil
}

func (w *Watcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	if w.fs != nil {
		w.fs.Close()
		w.fs = nil
	}
}

func (w *Watcher) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.fs != nil
}

func (w *Watcher) loop(ctx context.Context) {
	pending := make(map[string]time.Time)
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-w.fs.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Create|fsnotify.Write) == 0 {
				continue
			}
			ext := strings.ToLower(filepath.Ext(ev.Name))
			if ext != ".mkv" && ext != ".mp4" {
				continue
			}
			pending[ev.Name] = time.Now()
		case <-ticker.C:
			// Considère un fichier "stable" s'il n'a pas reçu d'event depuis 5s et que sa taille ne bouge plus
			for path, last := range pending {
				if time.Since(last) < 5*time.Second {
					continue
				}
				delete(pending, path)
				if w.onNewFile != nil {
					w.onNewFile(path)
				}
			}
		case <-w.fs.Errors:
			// silently ignore
		}
	}
}
