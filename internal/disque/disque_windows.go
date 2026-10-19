//go:build windows

package disque

import "golang.org/x/sys/windows"

// Libre renvoie les octets disponibles sur le volume qui contient chemin.
func Libre(chemin string) (int64, error) {
	p, err := windows.UTF16PtrFromString(chemin)
	if err != nil {
		return 0, err
	}
	var dispo, total, libreTotal uint64
	if err := windows.GetDiskFreeSpaceEx(p, &dispo, &total, &libreTotal); err != nil {
		return 0, err
	}
	return int64(dispo), nil
}
