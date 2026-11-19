package game

// Valeurs du personnage de départ et de la progression.
const (
	startHP          = 20
	startAttackBonus = 3
	startDexMod      = 1
	startWeaponName  = "Épée longue"
	startWeaponSlug  = "longsword"
	startWeaponDice  = "1d8"

	levelUpHP     = 6
	levelUpAttack = 1
	xpPerLevelSq  = 50
)

// Weapon est une arme. Slug l'identifie (inventaire, equip, sauvegarde).
type Weapon struct {
	Name   string
	Slug   string
	Damage Dice
}

// Character est le personnage joueur.
type Character struct {
	Name        string
	HP          int
	MaxHP       int
	Level       int
	XP          int
	AttackBonus int
	DexMod      int
	Gold        int
	Weapon      Weapon   // arme équipée ; elle figure aussi dans Inventory
	Inventory   []Weapon // armes possédées, dans l'ordre d'obtention
}

// LevelUp décrit un passage de niveau.
type LevelUp struct {
	Level       int
	MaxHP       int
	AttackBonus int
}

// NewCharacter crée un personnage de niveau 1.
func NewCharacter(name string) Character {
	c := Character{
		Name:        name,
		HP:          startHP,
		MaxHP:       startHP,
		Level:       1,
		AttackBonus: startAttackBonus,
		DexMod:      startDexMod,
	}
	c.Weapon = Weapon{Name: startWeaponName, Slug: startWeaponSlug, Damage: MustParseDice(startWeaponDice)}
	c.Inventory = []Weapon{c.Weapon}
	return c
}

// HasWeapon indique si l'arme de slug donné est dans l'inventaire.
func (c Character) HasWeapon(slug string) bool {
	_, ok := c.findWeapon(slug)
	return ok
}

func (c Character) findWeapon(slug string) (Weapon, bool) {
	for _, w := range c.Inventory {
		if w.Slug == slug {
			return w, true
		}
	}
	return Weapon{}, false
}

// XPThreshold renvoie l'XP totale nécessaire pour quitter le niveau level :
// level² × 50 (50 XP pour le niveau 2, 200 pour le 3, 450 pour le 4…).
func XPThreshold(level int) int { return level * level * xpPerLevelSq }

// NextLevelXP renvoie l'XP totale nécessaire pour le prochain niveau.
func (c Character) NextLevelXP() int { return XPThreshold(c.Level) }

// baseArmorClass correspond à une armure de cuir (11) ; le sujet ne définit
// pas de CA pour le joueur, elle est donc dérivée : 11 + modificateur de DEX.
const baseArmorClass = 11

// ArmorClass renvoie la classe d'armure du personnage (12 au départ).
func (c Character) ArmorClass() int { return baseArmorClass + c.DexMod }

// Alive indique si le personnage a encore des PV.
func (c Character) Alive() bool { return c.HP > 0 }

// HealFull rend tous ses PV au personnage et renvoie le nombre de PV rendus.
func (c *Character) HealFull() int {
	healed := c.MaxHP - c.HP
	c.HP = c.MaxHP
	return healed
}

// TakeDamage retire des PV sans descendre sous 0.
func (c *Character) TakeDamage(n int) {
	c.HP = max(0, c.HP-n)
}

// GainXP ajoute de l'XP et applique autant de montées de niveau que
// nécessaire. Chaque niveau donne +6 PV max, +1 au bonus d'attaque et un soin
// complet.
func (c *Character) GainXP(xp int) []LevelUp {
	if xp <= 0 {
		return nil
	}
	c.XP += xp
	var ups []LevelUp
	for c.XP >= XPThreshold(c.Level) {
		c.Level++
		c.MaxHP += levelUpHP
		c.AttackBonus += levelUpAttack
		c.HP = c.MaxHP
		ups = append(ups, LevelUp{Level: c.Level, MaxHP: c.MaxHP, AttackBonus: c.AttackBonus})
	}
	return ups
}
