package game

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Roller est la source d'aléa du jeu. *rand.Rand (math/rand/v2) la satisfait ;
// un test peut fournir une implémentation déterministe.
type Roller interface {
	// IntN renvoie un entier dans [0, n). n > 0.
	IntN(n int) int
}

// Limites raisonnables pour rejeter les expressions absurdes.
const (
	maxDiceCount = 100
	maxDiceSides = 1000
)

// ErrInvalidDice est enveloppée par toutes les erreurs de ParseDice.
var ErrInvalidDice = errors.New("expression de dés invalide")

type diceTerm struct {
	count, sides int
	sign         int // +1 ou -1
}

// Dice est une expression de dés parsée : "1d6", "2d8+3", "1d10-1", "2d10+1d8".
// La valeur zéro vaut 0.
type Dice struct {
	terms    []diceTerm
	modifier int
}

// D20 lance un dé à 20 faces.
func D20(r Roller) int { return r.IntN(20) + 1 }

// ParseDice parse une expression de la forme termes séparés par + ou -, chaque
// terme étant NdM ou une constante. Les espaces sont ignorés, "d6" vaut "1d6".
func ParseDice(expr string) (Dice, error) {
	s := strings.ToLower(strings.Join(strings.Fields(expr), ""))
	if s == "" {
		return Dice{}, fmt.Errorf("%w: expression vide", ErrInvalidDice)
	}

	var d Dice
	hasDie := false
	sign := 1
	i := 0
	if s[0] == '+' || s[0] == '-' {
		return Dice{}, fmt.Errorf("%w: %q commence par un signe", ErrInvalidDice, expr)
	}
	for i < len(s) {
		j := i
		for j < len(s) && s[j] != '+' && s[j] != '-' {
			j++
		}
		tok := s[i:j]
		if tok == "" {
			return Dice{}, fmt.Errorf("%w: %q contient un terme vide", ErrInvalidDice, expr)
		}
		if err := d.addTerm(tok, sign); err != nil {
			return Dice{}, fmt.Errorf("%w: %q: %w", ErrInvalidDice, expr, err)
		}
		if strings.Contains(tok, "d") {
			hasDie = true
		}
		if j == len(s) {
			break
		}
		if s[j] == '-' {
			sign = -1
		} else {
			sign = 1
		}
		i = j + 1
		if i == len(s) {
			return Dice{}, fmt.Errorf("%w: %q se termine par un signe", ErrInvalidDice, expr)
		}
	}
	if !hasDie {
		return Dice{}, fmt.Errorf("%w: %q ne contient aucun dé", ErrInvalidDice, expr)
	}
	return d, nil
}

// MustParseDice est réservée aux constantes du programme : elle panique si
// l'expression est invalide (erreur de programmation, pas de saisie).
func MustParseDice(expr string) Dice {
	d, err := ParseDice(expr)
	if err != nil {
		panic(err)
	}
	return d
}

func (d *Dice) addTerm(tok string, sign int) error {
	countStr, sidesStr, isDie := strings.Cut(tok, "d")
	if !isDie {
		n, err := strconv.Atoi(tok)
		if err != nil {
			return fmt.Errorf("constante %q: %w", tok, err)
		}
		d.modifier += sign * n
		return nil
	}
	count := 1
	if countStr != "" {
		n, err := strconv.Atoi(countStr)
		if err != nil {
			return fmt.Errorf("nombre de dés %q: %w", countStr, err)
		}
		count = n
	}
	sides, err := strconv.Atoi(sidesStr)
	if err != nil {
		return fmt.Errorf("nombre de faces %q: %w", sidesStr, err)
	}
	if count < 1 || count > maxDiceCount {
		return fmt.Errorf("nombre de dés %d hors de [1, %d]", count, maxDiceCount)
	}
	if sides < 1 || sides > maxDiceSides {
		return fmt.Errorf("nombre de faces %d hors de [1, %d]", sides, maxDiceSides)
	}
	d.terms = append(d.terms, diceTerm{count: count, sides: sides, sign: sign})
	return nil
}

// Plus renvoie une copie de d avec un modificateur supplémentaire.
func (d Dice) Plus(mod int) Dice {
	d.terms = append([]diceTerm(nil), d.terms...)
	d.modifier += mod
	return d
}

// Roll lance les dés et renvoie le total.
func (d Dice) Roll(r Roller) int { return d.roll(r, 1) }

// RollCritical lance les dés deux fois plus (coup critique) ; le
// modificateur fixe n'est pas doublé, comme en D&D 5e.
func (d Dice) RollCritical(r Roller) int { return d.roll(r, 2) }

func (d Dice) roll(r Roller, mult int) int {
	total := d.modifier
	for _, t := range d.terms {
		for range t.count * mult {
			total += t.sign * (r.IntN(t.sides) + 1)
		}
	}
	return total
}

// Average renvoie la valeur moyenne de l'expression (1d8 → 4.5, 2d6+1 → 8).
func (d Dice) Average() float64 {
	avg := float64(d.modifier)
	for _, t := range d.terms {
		avg += float64(t.sign*t.count) * float64(t.sides+1) / 2
	}
	return avg
}

// String renvoie la forme canonique de l'expression (ex. "2d8+3").
func (d Dice) String() string {
	var b strings.Builder
	for i, t := range d.terms {
		switch {
		case t.sign < 0:
			b.WriteByte('-')
		case i > 0:
			b.WriteByte('+')
		}
		fmt.Fprintf(&b, "%dd%d", t.count, t.sides)
	}
	switch {
	case d.modifier > 0:
		fmt.Fprintf(&b, "+%d", d.modifier)
	case d.modifier < 0:
		fmt.Fprintf(&b, "%d", d.modifier)
	}
	if b.Len() == 0 {
		return "0"
	}
	return b.String()
}
