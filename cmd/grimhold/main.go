// Commande grimhold : dungeon crawler textuel. main se contente de lire les
// flags, de construire les dépendances et de lancer le REPL.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand/v2"
	"os"
	"time"

	"github.com/Diangou/grimhold/internal/game"
	"github.com/Diangou/grimhold/internal/open5e"
	"github.com/Diangou/grimhold/internal/repl"
	"github.com/Diangou/grimhold/internal/world"
)

func main() {
	log.SetFlags(0)
	if err := run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr); err != nil {
		log.Fatalf("grimhold: %v", err)
	}
}

func run(args []string, stdin *os.File, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("grimhold", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		cont      = fs.Bool("continue", false, "charge directement la sauvegarde au lancement")
		worldPath = fs.String("world", "world.json", "chemin du fichier de monde")
		savePath  = fs.String("save", "save.json", "chemin du fichier de sauvegarde")
		name      = fs.String("name", "Errant", "nom du personnage (nouvelle partie)")
		seed      = fs.Uint64("seed", 0, "graine aléatoire (0 = aléatoire)")
		apiURL    = fs.String("api", open5e.DefaultBaseURL, "URL racine de l'API Open5e v1")
		cacheTTL  = fs.Duration("cache-ttl", open5e.DefaultCacheTTL, "durée de vie du cache HTTP")
	)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return fmt.Errorf("arguments: %w", err)
	}

	w, err := world.Load(*worldPath)
	if err != nil {
		return fmt.Errorf("chargement du monde: %w", err)
	}

	cache := open5e.NewCache(*cacheTTL, time.Minute)
	defer cache.Close()
	client, err := open5e.NewClient(*apiURL, cache)
	if err != nil {
		return fmt.Errorf("client Open5e: %w", err)
	}

	s := *seed
	if s == 0 {
		s = uint64(time.Now().UnixNano())
	}
	rng := rand.New(rand.NewPCG(s, s^0x9e3779b97f4a7c15))

	g, err := game.New(w, rng, *name)
	if err != nil {
		return fmt.Errorf("nouvelle partie: %w", err)
	}

	echo, err := isPipe(stdin)
	if err != nil {
		return err
	}
	session, err := repl.New(repl.Config{
		In:          stdin,
		Out:         stdout,
		Game:        g,
		Client:      client,
		SavePath:    *savePath,
		Echo:        echo,
		LoadOnStart: *cont,
	})
	if err != nil {
		return fmt.Errorf("initialisation du REPL: %w", err)
	}
	if err := session.Run(context.Background()); err != nil {
		return fmt.Errorf("REPL: %w", err)
	}
	return nil
}

// isPipe indique si l'entrée standard n'est pas un terminal (pipe ou
// fichier) : on réaffiche alors les commandes lues pour garder une
// transcription lisible.
func isPipe(f *os.File) (bool, error) {
	fi, err := f.Stat()
	if err != nil {
		return false, fmt.Errorf("inspection de l'entrée standard: %w", err)
	}
	return fi.Mode()&os.ModeCharDevice == 0, nil
}
