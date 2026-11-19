package game

import (
	"fmt"
	"math"
	"strings"

	"github.com/Diangou/grimhold/internal/open5e"
	"github.com/Diangou/grimhold/internal/world"
)

// Bornes du nombre d'attaques par round d'un boss : au moins 2 (c'est ce qui
// en fait un boss), au plus 3 (au-delà, le joueur n'a aucune chance).
const (
	minBossAttacks = 2
	maxBossAttacks = 3
)

// Attack est une attaque utilisable en combat.
type Attack struct {
	Name   string
	Bonus  int
	Damage Dice
}

// defaultAttack sert aux monstres sans aucune attaque exploitable.
var defaultAttack = Attack{Name: "Coup désespéré", Bonus: 0, Damage: MustParseDice("1d4")}

// Foe est un ennemi en combat, construit à partir d'une fiche Open5e.
type Foe struct {
	Name    string
	Slug    string
	CR      float64
	CRLabel string
	AC      int
	HP      int
	MaxHP   int
	DexMod  int
	Attacks []Attack

	// AttacksPerRound vaut 1 pour un ennemi ordinaire (une action au hasard
	// par round) et le nombre de sa Multiattack pour un boss.
	AttacksPerRound int
	Boss            bool
	Species         string // nom Open5e d'origine, utile quand Name est un titre
}

// AbilityMod calcule le modificateur D&D d'une caractéristique (10 → 0, 8 → -1).
func AbilityMod(score int) int {
	return int(math.Floor(float64(score-10) / 2))
}

// NewFoe convertit une fiche de monstre en ennemi. Seules les actions ayant un
// bonus d'attaque et des dés de dégâts valides deviennent des attaques ; s'il
// n'y en a aucune, l'ennemi reçoit une attaque par défaut de 1d4.
func NewFoe(m open5e.Monster) (Foe, error) {
	cr, err := m.ChallengeRating.Float()
	if err != nil {
		return Foe{}, fmt.Errorf("ennemi %q: %w", m.Slug, err)
	}
	if m.HitPoints <= 0 {
		return Foe{}, fmt.Errorf("ennemi %q: points de vie invalides (%d)", m.Slug, m.HitPoints)
	}
	f := Foe{
		Name:    m.Name,
		Slug:    m.Slug,
		CR:      cr,
		CRLabel: string(m.ChallengeRating),
		AC:      m.ArmorClass,
		HP:      m.HitPoints,
		MaxHP:   m.HitPoints,
		DexMod:  AbilityMod(m.Dexterity),

		AttacksPerRound: 1,
		Species:         m.Name,
	}
	for _, a := range m.Actions {
		if atk, ok := attackFromAction(a); ok {
			f.Attacks = append(f.Attacks, atk)
		}
	}
	if len(f.Attacks) == 0 {
		f.Attacks = []Attack{defaultAttack}
	}
	return f, nil
}

// NewBoss construit un boss à partir de sa fiche et de sa définition dans le
// monde : titre unique, PV éventuellement ajustés et plusieurs attaques par
// round tirées de sa Multiattack.
func NewBoss(m open5e.Monster, def world.Boss) (Foe, error) {
	f, err := NewFoe(m)
	if err != nil {
		return Foe{}, fmt.Errorf("boss %q: %w", def.Title, err)
	}
	f.Boss = true
	f.Name = def.Title
	if def.HitPoints > 0 {
		f.HP, f.MaxHP = def.HitPoints, def.HitPoints
	}
	f.AttacksPerRound = min(max(multiattackCount(m.Actions), minBossAttacks), maxBossAttacks)
	return f, nil
}

// multiattackCount lit le nombre d'attaques dans la description de l'action
// Multiattack ("The wight makes two longsword attacks…" → 2). Renvoie 1 si
// le monstre n'a pas de Multiattack lisible.
func multiattackCount(actions []open5e.Action) int {
	numbers := map[string]int{"two": 2, "three": 3, "four": 4, "five": 5}
	for _, a := range actions {
		if !strings.HasPrefix(strings.ToLower(a.Name), "multiattack") {
			continue
		}
		for _, w := range strings.Fields(strings.ToLower(a.Desc)) {
			if n, ok := numbers[strings.Trim(w, ".,:;")]; ok {
				return n
			}
		}
	}
	return 1
}

// attackFromAction renvoie false pour les actions qui ne sont pas des
// attaques (Multiattack, souffles sans dés…) ou dont les dés sont illisibles.
func attackFromAction(a open5e.Action) (Attack, bool) {
	if a.AttackBonus == nil || a.DamageDice == "" {
		return Attack{}, false
	}
	dmg, err := ParseDice(a.DamageDice)
	if err != nil {
		// Donnée de l'API inexploitable : on ignore simplement cette action.
		return Attack{}, false
	}
	if a.DamageBonus != nil {
		dmg = dmg.Plus(*a.DamageBonus)
	}
	return Attack{Name: a.Name, Bonus: *a.AttackBonus, Damage: dmg}, true
}

// Alive indique si l'ennemi tient encore debout.
func (f Foe) Alive() bool { return f.HP > 0 }
