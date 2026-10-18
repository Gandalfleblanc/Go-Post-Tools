package binutil

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	binaries  embed.FS
	initDone  bool
	appVer    string
	mu        sync.Mutex
	cacheDir  string
	cacheErr  error
	cacheOnce sync.Once
)

func Init(embedded embed.FS, version string) {
	mu.Lock()
	defer mu.Unlock()
	binaries = embedded
	appVer = version
	initDone = true
}

func ensureCacheDir() (string, error) {
	cacheOnce.Do(func() {
		home, e := os.UserHomeDir()
		if e != nil {
			cacheErr = e
			return
		}
		cacheDir = filepath.Join(home, ".cache", "go-post-tools", "bin")
		if cacheErr = os.MkdirAll(cacheDir, 0755); cacheErr != nil {
			return
		}
		purgeStaleCache(cacheDir)
	})
	return cacheDir, cacheErr
}

// purgeStaleCache vide le cache quand l'app a changé de version. Sans ça, un
// binaire extrait une fois n'est JAMAIS remplacé (voir le test de taille dans
// ExtractBinary) : un utilisateur qui a reçu un binaire cassé le garde même
// après mise à jour. Cas réel : parpar x86_64 livré par erreur dans la build
// arm64, inutilisable dès que Rosetta n'est plus installé.
func purgeStaleCache(dir string) {
	mu.Lock()
	want := appVer
	mu.Unlock()
	if want == "" {
		return
	}
	marker := filepath.Join(dir, ".version")
	if got, err := os.ReadFile(marker); err == nil && string(got) == want {
		return
	}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, ent := range entries {
			if !ent.IsDir() {
				_ = os.Remove(filepath.Join(dir, ent.Name()))
			}
		}
	}
	_ = os.WriteFile(marker, []byte(want), 0644)
}

// ExtractBinary extrait un binaire embarqué vers le cache et retourne son chemin.
// Retourne une erreur si le binaire n'est pas embarqué.
//
// Si un dossier compagnon `binaries/<platform>/<name>.d/` existe (ex : DLLs
// Windows), tous ses fichiers sont extraits à côté du binaire principal.
func ExtractBinary(name string) (string, error) {
	mu.Lock()
	ok := initDone
	mu.Unlock()
	if !ok {
		return "", fmt.Errorf("binutil non initialisé")
	}

	dir, err := ensureCacheDir()
	if err != nil {
		return "", fmt.Errorf("cache dir: %w", err)
	}

	binName := name
	if runtime.GOOS == "windows" {
		binName = name + ".exe"
	}
	destPath := filepath.Join(dir, binName)

	// Déjà extrait
	if info, err := os.Stat(destPath); err == nil && info.Size() > 1024 {
		return destPath, nil
	}

	platform := runtime.GOOS + "-" + runtime.GOARCH
	candidates := []string{
		"binaries/" + platform + "/" + binName,
		"binaries/" + binName,
	}

	var data []byte
	for _, p := range candidates {
		data, err = fs.ReadFile(binaries, p)
		if err == nil {
			break
		}
	}
	if err != nil {
		return "", fmt.Errorf("binaire %s non embarqué", name)
	}

	if err := os.WriteFile(destPath, data, 0755); err != nil {
		return "", fmt.Errorf("extraction %s: %w", name, err)
	}

	// Compagnons : binaries/<platform>/<name>.d/* → extraits dans le même
	// dossier cache que le binaire (ex : LIBCURL.DLL pour mediainfo Windows).
	companionDir := "binaries/" + platform + "/" + name + ".d"
	if entries, e := fs.ReadDir(binaries, companionDir); e == nil {
		for _, ent := range entries {
			if ent.IsDir() {
				continue
			}
			if cdata, ce := fs.ReadFile(binaries, companionDir+"/"+ent.Name()); ce == nil {
				_ = os.WriteFile(filepath.Join(dir, ent.Name()), cdata, 0644)
			}
		}
	}
	return destPath, nil
}

// ---------------------------------------------------------------------------
// Résolution tracée
//
// Les trois outils (parpar, nyuu, mediainfo) sont compilés DANS l'exécutable.
// Jusqu'ici, si l'extraction échouait, on retombait silencieusement sur le
// binaire du PATH : c'est ainsi qu'un parpar x86 installé par Homebrew a servi
// pendant des semaines via Rosetta, puis a disparu en changeant de Mac — sans
// que rien ne le signale. La résolution est désormais explicite et mémorisée,
// pour qu'un écart se voie immédiatement dans le Journal et les Réglages.
// ---------------------------------------------------------------------------

// Origine d'un binaire résolu.
const (
	SourceEmbarque = "embarqué"
	SourceSysteme  = "PATH système"
)

type Resolution struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	Source string `json:"source"`
	Err    string `json:"error,omitempty"`
}

var (
	resMu  sync.Mutex
	resMem = map[string]Resolution{}
)

// Resolve renvoie le chemin du binaire et d'où il vient. Le résultat est
// mémorisé : la résolution ne coûte qu'un appel, et reste stable pour un
// même lancement même si le cache est modifié entre-temps.
func Resolve(name string) Resolution {
	resMu.Lock()
	if r, ok := resMem[name]; ok {
		resMu.Unlock()
		return r
	}
	resMu.Unlock()

	r := Resolution{Name: name}
	if p, err := ExtractBinary(name); err == nil {
		r.Path, r.Source = p, SourceEmbarque
	} else if p, err2 := exec.LookPath(name); err2 == nil {
		r.Path, r.Source = p, SourceSysteme
		r.Err = err.Error()
	} else {
		r.Err = fmt.Sprintf("introuvable : ni embarqué (%v) ni dans le PATH", err)
	}

	resMu.Lock()
	resMem[name] = r
	resMu.Unlock()
	return r
}

// Path : chemin à exécuter. Renvoie le nom nu en dernier recours, pour que
// l'erreur d'exec reste explicite plutôt que de masquer un chemin vide.
func Path(name string) string {
	if r := Resolve(name); r.Path != "" {
		return r.Path
	}
	return name
}

// Statuses résout les outils demandés, dans l'ordre, pour affichage.
func Statuses(names ...string) []Resolution {
	out := make([]Resolution, 0, len(names))
	for _, n := range names {
		out = append(out, Resolve(n))
	}
	return out
}
