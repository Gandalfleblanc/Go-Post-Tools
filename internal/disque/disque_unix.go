//go:build !windows

// Package disque : espace libre sur le volume d'un chemin. Sert au contrôle
// avant décollage — une queue qui meurt à mi-parcours faute de place pour les
// PAR2 coûte bien plus cher que la seconde de vérification.
package disque

import "syscall"

// Libre renvoie les octets disponibles sur le volume qui contient chemin.
func Libre(chemin string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(chemin, &st); err != nil {
		return 0, err
	}
	// Bavail : blocs disponibles à un utilisateur NON privilégié — c'est ce
	// qui compte ici, pas Bfree qui inclut la réserve root.
	return int64(st.Bavail) * int64(st.Bsize), nil
}
