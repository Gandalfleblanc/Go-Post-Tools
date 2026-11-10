package main

import "testing"

func TestVersionPlusRecente(t *testing.T) {
	cas := []struct {
		a, b string
		veut bool
	}{
		{"10.4.2", "11.0.0", false}, // build locale en avance : pas de « mise à jour » vers l'arrière
		{"11.0.1", "11.0.0", true},
		{"11.0.0", "11.0.0", false},
		{"10.10.0", "10.9.0", true}, // une comparaison de chaînes se tromperait ici
		{"11.0", "11.0.0", false},
		{"12.0.0", "11.9.9", true},
	}
	for _, c := range cas {
		if got := versionPlusRecente(c.a, c.b); got != c.veut {
			t.Errorf("versionPlusRecente(%q, %q) = %v, attendu %v", c.a, c.b, got, c.veut)
		}
	}
}
