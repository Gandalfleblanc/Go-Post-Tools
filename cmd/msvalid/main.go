package main
import (
    "fmt"
    "go-post-tools/internal/config"
    "go-post-tools/internal/mediasearch"
)
func main() {
    fmt.Printf("URL: %q\n", config.DefaultMediaSearchURL)
    fmt.Printf("User: %q\n", config.DefaultLihdlUser)
    fmt.Printf("Pass: %q\n", config.DefaultLihdlPassword)
    r, err := mediasearch.Search(config.DefaultMediaSearchURL, "Backrooms 2026", config.DefaultLihdlUser, config.DefaultLihdlPassword)
    if err != nil { fmt.Println("ERR:", err); return }
    fmt.Printf("results: %d\n", len(r))
    for i, x := range r { if i > 2 { break }; fmt.Printf("[%d] %s (%s) tmdb=%d\n", i, x.TitleFR, x.Year, x.TmdbID) }
}
