// Package game contient les règles de Grimhold : personnage, dés, combat,
// butin, état de la partie et sauvegarde. Il ne fait aucune entrée/sortie
// terminal : les méthodes renvoient des rapports que la couche REPL met en
// forme.
package game

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/Diangou/grimhold/internal/open5e"
	"github.com/Diangou/grimhold/internal/world"
)

// Erreurs sentinelles du jeu.
var (
	ErrInCombat      = errors.New("tape fight ou flee")
	ErrNoCombat      = errors.New("aucun combat en cours")
	ErrAlreadyRested = errors.New("tu t'es déjà reposé ici ; voyage pour pouvoir te reposer à nouveau")
	ErrNothingHere   = errors.New("il n'y a rien à affronter ici")
	ErrLevelTooLow   = errors.New("un sceau de cendres bloque le passage")
)

// inCombat construit l'erreur renvoyée quand une action est interdite en combat.
func inCombat(action string) error {
	return fmt.Errorf("tu ne peux pas %s en plein combat (%w)", action, ErrInCombat)
}

// MonsterFetcher récupère une fiche de monstre par son slug (en pratique via
// le client Open5e et son cache). Injecté dans Explore pour la testabilité.
type MonsterFetcher func(slug string) (open5e.Monster, error)

// Encounter est un combat en cours.
type Encounter struct {
	Foe   Foe
	Round int
}

// Game est l'état complet d'une partie.
type Game struct {
	world  *world.World
	rng    Roller
	region string
	char   Character
	slain  map[string]int
	rested bool
	enc    *Encounter
	corpse *corpse
	// bosses contient les ids des régions dont le boss a été vaincu.
	bosses map[string]bool
}

// New démarre une partie dans la région de départ du monde.
func New(w *world.World, rng Roller, name string) (*Game, error) {
	if w == nil {
		return nil, errors.New("nouvelle partie: monde manquant")
	}
	if rng == nil {
		return nil, errors.New("nouvelle partie: générateur aléatoire manquant")
	}
	return &Game{
		world:  w,
		rng:    rng,
		region: w.Start(),
		char:   NewCharacter(name),
		slain:  make(map[string]int),
		bosses: make(map[string]bool),
	}, nil
}

// World renvoie la carte.
func (g *Game) World() *world.World { return g.world }

// Region renvoie la région actuelle.
func (g *Game) Region() world.Region {
	r, ok := g.world.Region(g.region)
	if !ok {
		// Invariant : g.region est toujours validée (New, Travel, Load).
		panic(fmt.Sprintf("game: région courante %q absente du monde", g.region))
	}
	return r
}

// Character renvoie une copie du personnage (inventaire compris).
func (g *Game) Character() Character {
	c := g.char
	c.Inventory = slices.Clone(g.char.Inventory)
	return c
}

// Slain renvoie une copie du bestiaire des victimes (slug → kills).
func (g *Game) Slain() map[string]int { return maps.Clone(g.slain) }

// BossDefeated indique si le boss de la région regionID a été vaincu.
func (g *Game) BossDefeated(regionID string) bool { return g.bosses[regionID] }

// CanRest indique si le repos est disponible dans la région actuelle.
func (g *Game) CanRest() bool { return !g.rested }

// InCombat indique si une rencontre est en cours.
func (g *Game) InCombat() bool { return g.enc != nil }

// Encounter renvoie la rencontre en cours, s'il y en a une.
func (g *Game) Encounter() (Encounter, bool) {
	if g.enc == nil {
		return Encounter{}, false
	}
	return *g.enc, true
}

// Travel déplace le joueur vers une région voisine. Une région peut exiger un
// niveau minimal. Le repos redevient disponible à chaque nouvelle visite, et
// le cadavre non fouillé est perdu.
func (g *Game) Travel(to string) (world.Region, error) {
	if g.InCombat() {
		return world.Region{}, inCombat("voyager")
	}
	if to == g.region {
		return world.Region{}, fmt.Errorf("tu es déjà à %s", g.Region().Name)
	}
	if err := g.world.CheckTravel(g.region, to); err != nil {
		return world.Region{}, fmt.Errorf("voyage impossible: %w", err)
	}
	dest, _ := g.world.Region(to) // existence garantie par CheckTravel
	if dest.MinLevel > g.char.Level {
		return world.Region{}, fmt.Errorf("%w : niveau %d requis pour entrer à %s (tu es niveau %d)",
			ErrLevelTooLow, dest.MinLevel, dest.Name, g.char.Level)
	}
	g.region = to
	g.rested = false
	g.corpse = nil
	return dest, nil
}

// Rest soigne complètement le personnage, une fois par visite de région.
// Renvoie le nombre de PV rendus.
func (g *Game) Rest() (int, error) {
	if g.InCombat() {
		return 0, inCombat("te reposer")
	}
	if g.rested {
		return 0, ErrAlreadyRested
	}
	g.rested = true
	return g.char.HealFull(), nil
}

// Explore déclenche une rencontre. Si la région a un boss encore en vie,
// c'est lui qui se présente. Sinon, une région de danger N fait N tirages
// parmi ses ennemis et garde celui au CR le plus élevé.
func (g *Game) Explore(fetch MonsterFetcher) (Foe, error) {
	if g.InCombat() {
		return Foe{}, fmt.Errorf("tu ne peux pas explorer en plein combat (%w)", ErrInCombat)
	}
	region := g.Region()
	if b := region.Boss; b != nil && !g.bosses[region.ID] {
		m, err := fetch(b.Slug)
		if err != nil {
			return Foe{}, fmt.Errorf("exploration de %s: %w", region.Name, err)
		}
		boss, err := NewBoss(m, *b)
		if err != nil {
			return Foe{}, fmt.Errorf("exploration de %s: %w", region.Name, err)
		}
		g.startEncounter(boss)
		return boss, nil
	}
	if len(region.Foes) == 0 {
		if region.Boss != nil {
			return Foe{}, fmt.Errorf("%s n'est plus qu'un tas de cendres ; %w", region.Boss.Title, ErrNothingHere)
		}
		return Foe{}, ErrNothingHere
	}

	draws := max(1, region.Danger)
	var best Foe
	found := false
	for range draws {
		slug := region.Foes[g.rng.IntN(len(region.Foes))]
		m, err := fetch(slug)
		if err != nil {
			return Foe{}, fmt.Errorf("exploration de %s: %w", region.Name, err)
		}
		foe, err := NewFoe(m)
		if err != nil {
			return Foe{}, fmt.Errorf("exploration de %s: %w", region.Name, err)
		}
		if !found || foe.CR > best.CR {
			best, found = foe, true
		}
	}
	g.startEncounter(best)
	return best, nil
}

func (g *Game) startEncounter(f Foe) {
	g.corpse = nil // on abandonne le cadavre précédent en repartant à la chasse
	g.enc = &Encounter{Foe: f}
}

// Outcome est l'issue d'un round ou d'une fuite.
type Outcome int

// Issues possibles.
const (
	OutcomeOngoing Outcome = iota
	OutcomeVictory
	OutcomeDefeat
	OutcomeFled
)

// AttackResult détaille un jet d'attaque.
type AttackResult struct {
	Attacker string
	Weapon   string
	Natural  int // résultat brut du d20
	Bonus    int
	Total    int
	TargetAC int
	Hit      bool
	Critical bool // 20 naturel
	Fumble   bool // 1 naturel
	Damage   int
}

// Victory récapitule les gains d'une victoire.
type Victory struct {
	FoeName  string
	FoeSlug  string
	Boss     bool
	XP       int
	Gold     int
	Kills    int // nombre total de victoires contre ce monstre
	LevelUps []LevelUp
}

// RoundReport décrit un round de combat.
type RoundReport struct {
	Round       int
	FoeName     string
	Player      AttackResult
	FoeAttacks  []AttackResult // vide si l'ennemi est mort avant de riposter
	Outcome     Outcome
	Victory     *Victory
	PlayerHP    int
	PlayerMaxHP int
	FoeHP       int
	FoeMaxHP    int
	RespawnAt   string // nom de la région de réveil en cas de défaite
}

// FleeReport décrit une tentative de fuite.
type FleeReport struct {
	FoeName    string
	Natural    int
	Mod        int
	Total      int
	DC         int
	Success    bool
	FreeAttack *AttackResult // attaque d'opportunité si la fuite échoue
	Outcome    Outcome       // OutcomeFled ou OutcomeDefeat
	PlayerHP   int
	RespawnAt  string
}

// minDamageOnHit garantit qu'un coup réussi blesse toujours un peu (un 1d10-1
// pourrait sinon faire 0 dégât sur un « coup »).
const minDamageOnHit = 1

func (g *Game) resolveAttack(attacker, weapon string, bonus int, dmg Dice, ac int) AttackResult {
	nat := D20(g.rng)
	res := AttackResult{
		Attacker: attacker,
		Weapon:   weapon,
		Natural:  nat,
		Bonus:    bonus,
		Total:    nat + bonus,
		TargetAC: ac,
	}
	switch nat {
	case 1:
		res.Fumble = true
	case 20:
		res.Hit, res.Critical = true, true
	default:
		res.Hit = res.Total >= ac
	}
	if res.Hit {
		n := dmg.Roll(g.rng)
		if res.Critical {
			n = dmg.RollCritical(g.rng)
		}
		res.Damage = max(minDamageOnHit, n)
	}
	return res
}

// foeAttack fait attaquer l'ennemi avec une de ses attaques, au hasard.
func (g *Game) foeAttack() AttackResult {
	foe := g.enc.Foe
	atk := foe.Attacks[g.rng.IntN(len(foe.Attacks))]
	res := g.resolveAttack(foe.Name, atk.Name, atk.Bonus, atk.Damage, g.char.ArmorClass())
	if res.Hit {
		g.char.TakeDamage(res.Damage)
	}
	return res
}

// Fight joue un round : le joueur attaque, puis l'ennemi riposte s'il
// survit, une fois (ennemi ordinaire) ou plusieurs (boss).
func (g *Game) Fight() (RoundReport, error) {
	if g.enc == nil {
		return RoundReport{}, fmt.Errorf("%w ; tape explore pour chercher un adversaire", ErrNoCombat)
	}
	g.enc.Round++
	foe := &g.enc.Foe
	rep := RoundReport{Round: g.enc.Round, FoeName: foe.Name, FoeMaxHP: foe.MaxHP}

	rep.Player = g.resolveAttack(g.char.Name, g.char.Weapon.Name, g.char.AttackBonus, g.char.Weapon.Damage, foe.AC)
	if rep.Player.Hit {
		foe.HP = max(0, foe.HP-rep.Player.Damage)
	}

	if !foe.Alive() {
		rep.Outcome = OutcomeVictory
		v := g.win()
		rep.Victory = &v
	} else {
		for i := 0; i < foe.AttacksPerRound && g.char.Alive(); i++ {
			rep.FoeAttacks = append(rep.FoeAttacks, g.foeAttack())
		}
		if !g.char.Alive() {
			rep.Outcome = OutcomeDefeat
			rep.RespawnAt = g.die()
		}
	}

	rep.PlayerHP, rep.PlayerMaxHP = g.char.HP, g.char.MaxHP
	rep.FoeHP = foe.HP
	if rep.Outcome != OutcomeOngoing {
		g.enc = nil
	}
	return rep, nil
}

// Flee tente une fuite : d20 + DEX contre 10 + DEX de l'ennemi. En cas
// d'échec, l'ennemi porte une attaque gratuite avant que le joueur ne
// s'échappe ; s'il survit, la fuite a lieu quand même. Fuir un boss le
// laisse en vie, avec tous ses PV, pour la prochaine tentative.
func (g *Game) Flee() (FleeReport, error) {
	if g.enc == nil {
		return FleeReport{}, ErrNoCombat
	}
	foe := g.enc.Foe
	nat := D20(g.rng)
	rep := FleeReport{
		FoeName: foe.Name,
		Natural: nat,
		Mod:     g.char.DexMod,
		Total:   nat + g.char.DexMod,
		DC:      10 + foe.DexMod,
		Outcome: OutcomeFled,
	}
	rep.Success = rep.Total >= rep.DC

	if !rep.Success {
		fa := g.foeAttack()
		rep.FreeAttack = &fa
		if !g.char.Alive() {
			rep.Outcome = OutcomeDefeat
			rep.RespawnAt = g.die()
		}
	}
	rep.PlayerHP = g.char.HP
	g.enc = nil
	return rep, nil
}

// win applique les récompenses de la victoire en cours et laisse un cadavre
// à fouiller.
func (g *Game) win() Victory {
	foe := g.enc.Foe
	g.slain[foe.Slug]++
	gold := 1 + g.rng.IntN(10) + int(foe.CR*10)
	if foe.Boss {
		gold *= bossGoldMultipler
		g.bosses[g.region] = true
	}
	g.char.Gold += gold
	g.corpse = &corpse{name: foe.Name, cr: foe.CR, boss: foe.Boss}
	xp := XPForCR(foe.CR)
	return Victory{
		FoeName:  foe.Name,
		FoeSlug:  foe.Slug,
		Boss:     foe.Boss,
		XP:       xp,
		Gold:     gold,
		Kills:    g.slain[foe.Slug],
		LevelUps: g.char.GainXP(xp),
	}
}

// die renvoie le joueur au point de départ avec 1 PV. Rien n'est écrit sur
// le disque : la sauvegarde existante reste intacte.
func (g *Game) die() string {
	g.region = g.world.Start()
	g.char.HP = 1
	g.rested = false
	g.corpse = nil
	return g.Region().Name
}
