package game

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Diangou/grimhold/internal/open5e"
)

// Paramètres du butin.
const (
	lootBaseChance    = 35  // % de trouver une arme sur un CR 0
	lootChancePerCR   = 10  // % supplémentaires par point de CR
	lootMaxChance     = 90  // plafond pour un ennemi ordinaire
	lootBaseAvg       = 4.5 // dégâts moyens max d'une arme sur un CR 0 (1d8)
	lootAvgPerCR      = 1.5 // dégâts moyens max supplémentaires par point de CR
	lootWindow        = 3.0 // largeur de la fourchette sous le plafond
	bossLootMinAvg    = 6.5 // un boss lâche une arme lourde (2d6, 1d12…)
	resaleGoldPerAvg  = 3   // or obtenu par point de dégâts moyens d'un doublon
	bossGoldMultipler = 5
)

// Erreurs du butin et de l'équipement.
var (
	ErrNothingToLoot = errors.New("il n'y a aucun cadavre à fouiller ici ; gagne d'abord un combat")
	ErrUnknownWeapon = errors.New("tu ne possèdes pas cette arme")
)

// corpse est le cadavre du dernier ennemi vaincu, fouillable avec Loot tant
// que le joueur ne quitte pas la région ni ne relance une exploration.
type corpse struct {
	name string
	cr   float64
	boss bool
}

// LootReport décrit le résultat d'une fouille.
type LootReport struct {
	FoeName string
	Weapon  *Weapon // nil si rien trouvé
	New     bool    // false : doublon revendu
	Gold    int     // or tiré de la revente d'un doublon
}

// ArmoryFromAPI convertit les armes Open5e en armes jouables. Les armes sans
// vraie expression de dés (sarbacane "1", filet "0") sont écartées.
func ArmoryFromAPI(ws []open5e.Weapon) []Weapon {
	out := make([]Weapon, 0, len(ws))
	for _, w := range ws {
		dmg, err := ParseDice(w.DamageDice)
		if err != nil || w.Slug == "" {
			continue // donnée inexploitable pour le combat : pas une arme de butin
		}
		out = append(out, Weapon{Name: w.Name, Slug: w.Slug, Damage: dmg})
	}
	return out
}

// HasCorpse indique si un cadavre attend d'être fouillé.
func (g *Game) HasCorpse() bool { return g.corpse != nil }

// Loot fouille le cadavre du dernier ennemi vaincu. armory est la liste des
// armes possibles (en pratique, les armes du SRD). Un ennemi plus puissant a
// plus de chances de lâcher une arme, et une meilleure ; un boss lâche
// toujours une arme lourde. Un doublon est revendu contre de l'or.
func (g *Game) Loot(armory []Weapon) (LootReport, error) {
	if g.InCombat() {
		return LootReport{}, inCombat("fouiller un cadavre")
	}
	if g.corpse == nil {
		return LootReport{}, ErrNothingToLoot
	}
	if len(armory) == 0 {
		return LootReport{}, errors.New("fouille: aucune arme disponible dans l'armurerie")
	}
	c := *g.corpse
	g.corpse = nil // un cadavre ne se fouille qu'une fois
	rep := LootReport{FoeName: c.name}

	chance := min(lootBaseChance+int(c.cr*lootChancePerCR), lootMaxChance)
	if c.boss {
		chance = 100
	}
	if g.rng.IntN(100) >= chance {
		return rep, nil
	}

	candidates := lootCandidates(armory, c)
	w := candidates[g.rng.IntN(len(candidates))]
	rep.Weapon = &w
	if g.char.HasWeapon(w.Slug) {
		rep.Gold = max(1, int(w.Damage.Average()*resaleGoldPerAvg))
		g.char.Gold += rep.Gold
		return rep, nil
	}
	rep.New = true
	g.char.Inventory = append(g.char.Inventory, w)
	return rep, nil
}

// lootCandidates filtre l'armurerie selon la puissance de l'ennemi : ses
// dégâts moyens doivent tomber dans [plafond-lootWindow, plafond], où le
// plafond croît avec le CR. Un ennemi fort lâche donc une arme forte. Si le
// filtre ne laisse rien, toute l'armurerie est utilisée.
func lootCandidates(armory []Weapon, c corpse) []Weapon {
	ceiling := lootBaseAvg + c.cr*lootAvgPerCR
	var out []Weapon
	for _, w := range armory {
		avg := w.Damage.Average()
		if c.boss && avg >= bossLootMinAvg || !c.boss && avg <= ceiling && avg >= ceiling-lootWindow {
			out = append(out, w)
		}
	}
	if len(out) == 0 {
		return armory
	}
	return out
}

// Equip équipe une arme de l'inventaire, désignée par son slug ou son nom.
// Interdit en combat : pas le temps de changer d'arme.
func (g *Game) Equip(query string) (Weapon, error) {
	if g.InCombat() {
		return Weapon{}, inCombat("changer d'arme")
	}
	slug := open5e.Slugify(query)
	for _, w := range g.char.Inventory {
		if w.Slug == slug || strings.EqualFold(w.Name, strings.TrimSpace(query)) {
			g.char.Weapon = w
			return w, nil
		}
	}
	names := make([]string, len(g.char.Inventory))
	for i, w := range g.char.Inventory {
		names[i] = w.Slug
	}
	return Weapon{}, fmt.Errorf("%w : %q (inventaire : %s)", ErrUnknownWeapon, query, strings.Join(names, ", "))
}
