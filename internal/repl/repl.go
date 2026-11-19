// Package repl implémente la boucle interactive de Grimhold : lecture des
// lignes, découpage, registre des commandes et affichage.
package repl

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Diangou/grimhold/internal/game"
	"github.com/Diangou/grimhold/internal/open5e"
)

const (
	prompt = "grimhold > "
	// maxLineSize borne la taille d'une ligne saisie (le défaut de
	// bufio.Scanner, 64 Kio, est relevé à 1 Mio).
	maxLineSize = 1 << 20
	// unlimited indique qu'une commande accepte un nombre quelconque d'arguments.
	unlimited = -1
)

// errQuit est renvoyée par quit/exit pour terminer la boucle proprement.
var errQuit = errors.New("quit")

// cliCommand décrit une commande du REPL. Le callback reçoit la session, ce
// qui évite les variables globales et les closures sur un état caché.
type cliCommand struct {
	name        string
	description string
	usage       string
	minArgs     int
	maxArgs     int // unlimited pour « pas de maximum »
	callback    func(s *Session, args []string) error
}

// Config regroupe les dépendances d'une session.
type Config struct {
	In       io.Reader
	Out      io.Writer
	Game     *game.Game
	Client   *open5e.Client
	SavePath string
	// Echo réaffiche chaque commande lue après le prompt : utile quand
	// l'entrée est un pipe, pour que la transcription reste lisible.
	Echo bool
	// LoadOnStart charge la sauvegarde avant la première commande (--continue).
	LoadOnStart bool
}

// Session est une partie interactive en cours.
type Session struct {
	ctx         context.Context
	in          io.Reader
	out         *console
	game        *game.Game
	client      *open5e.Client
	savePath    string
	echo        bool
	loadOnStart bool
	commands    map[string]cliCommand

	// Pagination du bestiaire.
	page    *open5e.MonsterList
	pageNum int

	deaths int
}

// New construit une session à partir de sa configuration.
func New(cfg Config) (*Session, error) {
	switch {
	case cfg.In == nil || cfg.Out == nil:
		return nil, errors.New("repl: entrée et sortie requises")
	case cfg.Game == nil:
		return nil, errors.New("repl: partie requise")
	case cfg.Client == nil:
		return nil, errors.New("repl: client Open5e requis")
	case cfg.SavePath == "":
		return nil, errors.New("repl: chemin de sauvegarde requis")
	}
	return &Session{
		ctx:         context.Background(),
		in:          cfg.In,
		out:         &console{w: cfg.Out},
		game:        cfg.Game,
		client:      cfg.Client,
		savePath:    cfg.SavePath,
		echo:        cfg.Echo,
		loadOnStart: cfg.LoadOnStart,
		commands:    newCommands(),
	}, nil
}

// Run exécute la boucle jusqu'à quit/exit ou la fin de l'entrée (EOF).
// Elle ne renvoie une erreur que pour un problème d'entrée/sortie.
func (s *Session) Run(ctx context.Context) error {
	s.ctx = ctx
	s.out.Println(banner)
	if s.loadOnStart {
		s.dispatch("load", nil)
	}

	scanner := bufio.NewScanner(s.in)
	scanner.Buffer(make([]byte, 0, 4096), maxLineSize)
	for {
		s.out.Print(prompt)
		if err := s.out.Err(); err != nil {
			return err
		}
		if !scanner.Scan() {
			break
		}
		line := scanner.Text()
		if s.echo {
			s.out.Println(line)
		}
		quit := s.handleLine(line)
		if err := s.out.Err(); err != nil {
			return err
		}
		if quit {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("lecture de l'entrée: %w", err)
	}
	// EOF (Ctrl+D) : sortie propre.
	s.out.Println()
	s.out.Println(farewell)
	return s.out.Err()
}

// handleLine découpe et exécute une ligne. Renvoie true pour quitter.
func (s *Session) handleLine(line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	return s.dispatch(strings.ToLower(fields[0]), fields[1:])
}

// dispatch exécute la commande name. Renvoie true pour quitter.
func (s *Session) dispatch(name string, args []string) bool {
	cmd, ok := s.commands[name]
	if !ok {
		s.out.Printf("Commande inconnue : %q. Tape help pour voir les commandes.\n", name)
		return false
	}
	if len(args) < cmd.minArgs || (cmd.maxArgs != unlimited && len(args) > cmd.maxArgs) {
		s.out.Printf("Usage : %s\n", cmd.usage)
		return false
	}
	err := s.exec(cmd, args)
	switch {
	case errors.Is(err, errQuit):
		return true
	case err != nil:
		s.out.Printf("✗ %v\n", err)
	}
	return false
}

// exec appelle le callback avec un recover en filet de sécurité : un bug dans
// une commande ne doit jamais faire tomber le REPL.
func (s *Session) exec(cmd cliCommand, args []string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("erreur interne pendant %q (%v) ; la partie continue", cmd.name, r)
		}
	}()
	return cmd.callback(s, args)
}

const banner = `
  ▄████  ██▀███   ██▓ ███▄ ▄███▓ ██░ ██  ▒█████   ██▓    ▓█████▄
 ██▒ ▀█▒▓██ ▒ ██▒▓██▒▓██▒▀█▀ ██▒▓██░ ██▒▒██▒  ██▒▓██▒    ▒██▀ ██▌
▒██░▄▄▄░▓██ ░▄█ ▒▒██▒▓██    ▓██░▒██▀▀██░▒██░  ██▒▒██░    ░██   █▌
░▓█  ██▓▒██▀▀█▄  ░██░▒██    ▒██ ░▓█ ░██ ▒██   ██░▒██░    ░▓█▄   ▌
░▒▓███▀▒░██▓ ▒██▒░██░▒██▒   ░██▒░▓█▒░██▓░ ████▓▒░░██████▒░▒████▓

Les terres sont maudites. Tape help pour connaître tes options.`

const farewell = "La brume se referme derrière toi. À bientôt, si tu survis."
