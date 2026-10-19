package tmdb

import "testing"

func TestNormaliserBase(t *testing.T) {
	cas := []struct{ in, want string }{
		{"https://tmdb.uklm.xyz", "https://tmdb.uklm.xyz/api"},           // le cas qui a coûté une minute d'attente
		{"https://tmdb.uklm.xyz/", "https://tmdb.uklm.xyz/api"},
		{"  https://tmdb.uklm.xyz  ", "https://tmdb.uklm.xyz/api"},
		{"https://elysium-les5zamis.com/tmdb-api", "https://elysium-les5zamis.com/tmdb-api"}, // chemin déjà présent
		{"https://tmdb.uklm.xyz/api", "https://tmdb.uklm.xyz/api"},
		{"pas une url", "pas une url"},
	}
	for _, c := range cas {
		if got := normaliserBase(c.in); got != c.want {
			t.Errorf("normaliserBase(%q) = %q, attendu %q", c.in, got, c.want)
		}
	}
}

func TestEstHTML(t *testing.T) {
	if !estHTML([]byte("<!doctype html>\n<html>")) {
		t.Error("page HTML non détectée")
	}
	if estHTML([]byte(`{"results":[]}`)) {
		t.Error("JSON pris pour du HTML")
	}
}
