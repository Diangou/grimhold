package repl

import (
	"fmt"
	"io"
)

// console écrit sur la sortie en conservant la première erreur d'écriture
// (même principe que bufio.Writer). Les commandes peuvent ainsi afficher sans
// vérifier chaque appel, et la boucle du REPL contrôle Err() après chaque
// commande : si le terminal disparaît, le REPL s'arrête avec cette erreur au
// lieu de l'ignorer.
type console struct {
	w   io.Writer
	err error
}

func (c *console) Printf(format string, a ...any) {
	if c.err != nil {
		return
	}
	if _, err := fmt.Fprintf(c.w, format, a...); err != nil {
		c.err = fmt.Errorf("écriture sur la sortie: %w", err)
	}
}

func (c *console) Println(a ...any) {
	if c.err != nil {
		return
	}
	if _, err := fmt.Fprintln(c.w, a...); err != nil {
		c.err = fmt.Errorf("écriture sur la sortie: %w", err)
	}
}

func (c *console) Print(s string) {
	if c.err != nil {
		return
	}
	if _, err := io.WriteString(c.w, s); err != nil {
		c.err = fmt.Errorf("écriture sur la sortie: %w", err)
	}
}

// Err renvoie la première erreur d'écriture rencontrée.
func (c *console) Err() error { return c.err }
