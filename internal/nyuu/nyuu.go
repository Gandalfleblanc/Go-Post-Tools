package nyuu

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go-post-tools/internal/binutil"
	"go-post-tools/internal/config"
)

type Progress struct {
	Percent  float64 `json:"percent"`
	Articles string  `json:"articles"`
	Speed    string  `json:"speed"`
	ETA      string  `json:"eta"`
	Done     bool    `json:"done"`
	Error    string  `json:"error,omitempty"`
}

type Result struct {
	NZBPath string `json:"nzb_path"`
	// Warnings : non vide si nyuu a terminé en code 32 (post complet, mais des
	// erreurs ont été ignorées grâce à --skip-errors). Le NZB est exploitable,
	// mais l'utilisateur doit être prévenu.
	Warnings string `json:"warnings,omitempty"`
}

var (
	progressRegex = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*%`)
	articlesRegex = regexp.MustCompile(`(\d+)\s*/\s*(\d+)`)
	speedRegex    = regexp.MustCompile(`(\d+(?:\.\d+)?)\s*([KMGT]i?B/s|B/s)`)
	etaRegex      = regexp.MustCompile(`ETA\s+(\d{2}:\d{2}(?::\d{2})?)`)
	// notableRegex : lignes qui expliquent une panne, par opposition au bruit de
	// progression. Ce sont elles qu'on remonte en tête du message d'erreur.
	// Libellés exacts de nyuu v0.4.2 : `[ERR ]`, `[WARN]` (et `[INFO]`, `[DBG ]`
	// qu'on ignore) — noter l'espace de padding dans `[ERR ]`.
	notableRegex = regexp.MustCompile(`\[(ERR|WARN|CRIT)\s*\]`)
	ansiRegex    = regexp.MustCompile("\x1b\\[[0-9;?]*[A-Za-z]")
)

// exitCodeNyuuSkipped : code retour nyuu quand le post s'est terminé mais que
// des erreurs ont été ignorées (cf. `nyuu --help-full`, section Exit codes).
const exitCodeNyuuSkipped = 32

// rawTailSize : nombre de lignes brutes conservées en secours quand nyuu meurt
// sans avoir émis la moindre ligne [ERROR]. Borné pour ne pas accumuler les
// milliers de lignes de progression d'un gros post en mémoire.
const rawTailSize = 20

func binaryPath() string {
	if path, err := binutil.ExtractBinary("nyuu"); err == nil {
		return path
	}
	if path, err := exec.LookPath("nyuu"); err == nil {
		return path
	}
	return "nyuu"
}

var errAccountBusy = errors.New("toutes les connexions du compte Usenet sont occupées")

// probeFreeSlot ouvre UNE connexion de contrôle et dit si le compte accepte
// encore une connexion. Renvoie errAccountBusy sur un 482.
//
// La socket est refermée « passivement » (QUIT puis lecture jusqu'à EOF) pour
// laisser le serveur fermer en premier : sinon elle resterait en TIME_WAIT
// côté client et occuperait justement un des slots qu'on cherche à libérer.
func probeFreeSlot(cfg *config.Config) error {
	addr := net.JoinHostPort(cfg.UsenetHost, strconv.Itoa(cfg.UsenetPort))
	dialer := &net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	var err error
	if cfg.UsenetSSL {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: cfg.UsenetHost})
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(25 * time.Second))

	br := bufio.NewReader(conn)
	if _, err := br.ReadString('\n'); err != nil { // bannière
		return err
	}
	fmt.Fprintf(conn, "AUTHINFO USER %s\r\n", cfg.UsenetUser)
	if _, err := br.ReadString('\n'); err != nil {
		return err
	}
	fmt.Fprintf(conn, "AUTHINFO PASS %s\r\n", cfg.UsenetPassword)
	line, err := br.ReadString('\n')
	if err != nil {
		return err
	}
	fmt.Fprint(conn, "QUIT\r\n")
	_, _ = io.Copy(io.Discard, br)

	switch {
	case strings.HasPrefix(line, "281"):
		return nil
	case strings.HasPrefix(line, "48"):
		return errAccountBusy
	default:
		return fmt.Errorf("auth NNTP : %s", strings.TrimSpace(line))
	}
}

// waitForFreeSlot attend que le compte Usenet ait des connexions disponibles
// avant de lancer nyuu.
//
// Pourquoi : nyuu ouvre d'emblée toutes les connexions configurées. Si le post
// précédent n'a pas encore été relâché par le provider, il se prend un 482 et
// abandonne — et s'il réessaie, il rouvre autant de connexions, ce qui empêche
// le provider de libérer les anciennes : le blocage s'auto-entretient. Attendre
// poliment avec UNE connexion de contrôle laisse le compte se libérer, tout en
// gardant le plein débit (toutes les connexions) une fois le post lancé.
func waitForFreeSlot(ctx context.Context, cfg *config.Config, onStatus func(string)) {
	if cfg.UsenetHost == "" || cfg.UsenetUser == "" {
		return
	}
	const maxWait = 5 * time.Minute
	const every = 20 * time.Second
	deadline := time.Now().Add(maxWait)

	for attempt := 0; ; attempt++ {
		err := probeFreeSlot(cfg)
		if err == nil {
			if attempt > 0 && onStatus != nil {
				onStatus("Connexions Usenet libérées — démarrage du post…")
			}
			return
		}
		// Erreur réseau/auth : ce n'est pas un problème de saturation, on laisse
		// nyuu tenter et rapporter l'erreur réelle.
		if !errors.Is(err, errAccountBusy) {
			return
		}
		if time.Now().After(deadline) {
			if onStatus != nil {
				onStatus("Connexions Usenet toujours occupées après 5 min — tentative quand même…")
			}
			return
		}
		if onStatus != nil {
			onStatus(fmt.Sprintf("Connexions Usenet occupées par le post précédent — attente %s…",
				time.Until(deadline).Round(time.Second)))
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(every):
		}
	}
}

func Run(ctx context.Context, cfg *config.Config, inputFiles []string, nzbOutputPath string, releaseName string, onProgress func(Progress), onStatus func(string)) (*Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	waitForFreeSlot(ctx, cfg, onStatus)
	args := buildArgs(cfg, inputFiles, nzbOutputPath, releaseName)
	cmd := exec.CommandContext(ctx, binaryPath(), args...)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("pipe stderr: %w", err)
	}
	cmd.Stdout = io.Discard

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("démarrage nyuu: %w", err)
	}

	notable, rawTail := parseProgress(stderr, onProgress)

	waitErr := cmd.Wait()
	code := exitCode(waitErr)

	// Code 32 = le post est allé au bout, mais --skip-errors a laissé passer des
	// incidents (timeout, connexion perdue, check raté). Le NZB est utilisable :
	// on rend la main en succès, en remontant les incidents à l'appelant.
	if waitErr != nil && code != exitCodeNyuuSkipped {
		msg := summarize(notable, rawTail)
		// 482 = toutes les connexions du compte sont prises. Le plus souvent
		// par le post précédent : le provider met un moment à les relâcher.
		// Message explicite, sinon l'utilisateur ne sait pas qu'il suffit
		// d'attendre — et relancer tout de suite ne fait qu'entretenir le blocage.
		if strings.Contains(msg, "482") || strings.Contains(strings.ToLower(msg), "too many connections") {
			msg += "\n→ Toutes les connexions du compte Usenet sont occupées, " +
				"en général par le post précédent. Attends 2-3 minutes SANS rien relancer, " +
				"puis réessaie : relancer immédiatement empêche le provider de les libérer."
		}
		onProgress(Progress{Done: true, Error: msg})
		return nil, fmt.Errorf("nyuu (code %d) : %s", code, msg)
	}

	onProgress(Progress{Percent: 100, Done: true})
	res := &Result{NZBPath: nzbOutputPath}
	if code == exitCodeNyuuSkipped {
		res.Warnings = summarize(notable, rawTail)
	}
	return res, nil
}

// exitCode extrait le code retour du process. Renvoie -1 si l'échec vient
// d'autre chose que d'une sortie non nulle (process tué, pipe cassé…).
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return -1
}

// summarize construit un message lisible : les lignes [ERROR]/[WARN] d'abord,
// puisque c'est là qu'est la cause. Sans aucune, on retombe sur les dernières
// lignes brutes plutôt que de ne rien dire.
func summarize(notable, rawTail []string) string {
	if len(notable) > 0 {
		return strings.Join(notable, "\n")
	}
	if len(rawTail) > 0 {
		return strings.Join(rawTail, "\n")
	}
	return "aucune sortie de nyuu"
}

func buildArgs(cfg *config.Config, inputFiles []string, nzbOutputPath string, releaseName string) []string {
	group := cfg.UsenetGroup
	if group == "" {
		group = "alt.binaries.test"
	}
	// On utilise la TOTALITÉ des connexions configurées : c'est ce qui donne le
	// débit (~40 Mo/s à 25 connexions sur un gros fichier). Le risque de 482
	// « too many connections » est traité en amont par waitForFreeSlot(), qui
	// attend que le provider ait relâché les connexions du post précédent au
	// lieu de lancer nyuu à l'aveugle et de le faire échouer.
	conns := cfg.UsenetConns
	if conns <= 0 {
		conns = 20
	}

	args := []string{
		"-h", cfg.UsenetHost,
		"-P", strconv.Itoa(cfg.UsenetPort),
		"-u", cfg.UsenetUser,
		"-p", cfg.UsenetPassword,
		"-n", strconv.Itoa(conns),
		"-g", group,
		"-o", nzbOutputPath,
		"--nzb-title", releaseName,
		"-f", "{rand(14)} {rand(14)}@{rand(5)}.{rand(3)}",
		"--message-id", "{rand(32)}@{rand(8)}.{rand(3)}",
		"--subject", "{rand(32)}",
		"--nzb-subject", `[{0filenum}/{files}] - "{filename}" yEnc ({part}/{parts})`,
		"--obfuscate-articles",
		"--overwrite",
		"--progress=stderr",
		// Par défaut nyuu s'arrête à la PREMIÈRE erreur, quelle qu'elle soit : sur
		// un post de plusieurs milliers d'articles, un aléa réseau isolé suffit à
		// tout perdre (exit 33). On absorbe les incidents non destructifs. Volon-
		// tairement PAS `post-fail` : un article réellement non posté doit rester
		// fatal, sinon on publierait un NZB troué.
		"--skip-errors", "post-timeout,check-timeout,check-missing,check-fail,connect-fail",
		// Par défaut nyuu ne retente la connexion qu'UNE fois après 15 s, puis
		// abandonne tout le post (« No connections available for uploading »).
		//
		// ⚠ Le délai doit impérativement dépasser le TIME_WAIT de l'OS (30 s sur
		// macOS : net.inet.tcp.msl = 15 s, TIME_WAIT = 2×MSL). Une socket fermée
		// y reste bloquée, et le provider la compte TOUJOURS dans le quota de
		// connexions. Avec un délai plus court, nyuu retente avant que ses
		// PROPRES sockets précédentes soient libérées, se reprend un 482, les
		// referme, et boucle indéfiniment sans jamais établir une seule
		// connexion (observé : ESTABLISHED=0, TIME_WAIT oscillant 25↔50).
		// ⚠ Rester TRÈS sobre ici. Chaque tentative rouvre `conns` connexions ;
		// si le compte est déjà saturé, s'acharner empêche le provider de
		// libérer les précédentes et le blocage devient permanent (observé :
		// 10 tentatives × 90 s = 15 min de 482 en boucle, compte jamais
		// relâché ; à l'arrêt complet il redevient libre en ~2 min).
		// 2 tentatives espacées de 60 s absorbent un creux passager ; au-delà
		// on rend la main avec une erreur lisible plutôt que de marteler.
		"--connect-retries", "2",
		"--reconnect-delay", "60s",
	}

	if cfg.UsenetSSL {
		args = append(args, "-S")
	}

	args = append(args, inputFiles...)
	return args
}

// parseProgress consomme stderr et renvoie (lignes notables, dernières lignes
// brutes). Les codes ANSI sont retirés : le scanner en laisse passer (un `\x1b[0G`
// en début de chunk finissait recollé au texte, d'où les « [0G » dans les
// messages d'erreur remontés aux utilisateurs).
func parseProgress(r io.Reader, onProgress func(Progress)) (notable, rawTail []string) {
	scanner := bufio.NewScanner(r)
	scanner.Split(scanLines)

	seen := map[string]bool{}
	for scanner.Scan() {
		line := strings.TrimSpace(ansiRegex.ReplaceAllString(scanner.Text(), ""))
		if line == "" {
			continue
		}

		if notableRegex.MatchString(line) && !seen[line] {
			seen[line] = true
			notable = append(notable, line)
		}
		rawTail = append(rawTail, line)
		if len(rawTail) > rawTailSize {
			rawTail = rawTail[1:]
		}

		p := Progress{}
		updated := false

		if m := progressRegex.FindStringSubmatch(line); len(m) >= 2 {
			if pct, err := strconv.ParseFloat(m[1], 64); err == nil {
				p.Percent = pct
				updated = true
			}
		}
		if m := articlesRegex.FindStringSubmatch(line); len(m) >= 3 {
			p.Articles = m[1] + "/" + m[2]
			updated = true
		}
		if m := speedRegex.FindStringSubmatch(line); len(m) >= 3 {
			p.Speed = m[1] + " " + m[2]
			updated = true
		}
		if m := etaRegex.FindStringSubmatch(line); len(m) >= 2 {
			p.ETA = m[1]
			updated = true
		}
		if updated {
			onProgress(p)
		}
	}
	return notable, rawTail
}

func scanLines(data []byte, atEOF bool) (advance int, token []byte, err error) {
	if atEOF && len(data) == 0 {
		return 0, nil, nil
	}
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' || data[i] == '\r' {
			return i + 1, data[:i], nil
		}
		if data[i] == 0x1b && i+3 < len(data) && data[i+1] == '[' {
			for j := i + 2; j < len(data); j++ {
				if (data[j] >= 'A' && data[j] <= 'Z') || (data[j] >= 'a' && data[j] <= 'z') {
					if i > 0 {
						return j + 1, data[:i], nil
					}
					i = j
					break
				}
			}
		}
	}
	if atEOF {
		return len(data), data, nil
	}
	return 0, nil, nil
}
