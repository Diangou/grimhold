package game

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/Diangou/grimhold/internal/open5e"
)

// SaveVersion est la version du format de sauvegarde écrit par Save.
// v2 ajoute l'inventaire, le slug des armes et les boss vaincus ; Load
// accepte encore la v1 et la migre.
const SaveVersion = 2

// ErrNoSave est renvoyée par Load quand le fichier n'existe pas.
var ErrNoSave = errors.New("aucune sauvegarde trouvée")

// saveFile est le format disque. Il est volontairement découplé des structs
// internes : on peut faire évoluer Game sans casser les sauvegardes.
type saveFile struct {
	Version    int            `json:"version"`
	Region     string         `json:"region"`
	Character  saveCharacter  `json:"character"`
	Gold       int            `json:"gold"`
	Slain      map[string]int `json:"slain"`
	RestedHere bool           `json:"rested_here"`
	// BossesDefeated liste les ids des régions dont le boss est vaincu (v2).
	BossesDefeated []string `json:"bosses_defeated,omitempty"`
}

type saveCharacter struct {
	Name        string     `json:"name"`
	HP          int        `json:"hp"`
	MaxHP       int        `json:"max_hp"`
	Level       int        `json:"level"`
	XP          int        `json:"xp"`
	AttackBonus int        `json:"attack_bonus"`
	DexMod      int        `json:"dex_mod"`
	Weapon      saveWeapon `json:"weapon"` // arme équipée
	// Inventory liste toutes les armes possédées, équipée comprise (v2).
	Inventory []saveWeapon `json:"inventory,omitempty"`
}

type saveWeapon struct {
	Name   string `json:"name"`
	Slug   string `json:"slug,omitempty"` // absent en v1
	Damage string `json:"damage"`
}

func toSaveWeapon(w Weapon) saveWeapon {
	return saveWeapon{Name: w.Name, Slug: w.Slug, Damage: w.Damage.String()}
}

// fromSaveWeapon reconstruit une arme. En v1 le slug n'existait pas : il est
// dérivé du nom.
func fromSaveWeapon(sw saveWeapon) (Weapon, error) {
	if strings.TrimSpace(sw.Name) == "" {
		return Weapon{}, errors.New("arme sans nom")
	}
	dmg, err := ParseDice(sw.Damage)
	if err != nil {
		return Weapon{}, fmt.Errorf("arme %q: %w", sw.Name, err)
	}
	slug := sw.Slug
	if slug == "" {
		// Une sauvegarde v1 ne pouvait contenir que l'arme de départ.
		if sw.Name == startWeaponName {
			slug = startWeaponSlug
		} else {
			slug = open5e.Slugify(sw.Name)
		}
	}
	if slug == "" {
		return Weapon{}, fmt.Errorf("arme %q: slug vide", sw.Name)
	}
	return Weapon{Name: sw.Name, Slug: slug, Damage: dmg}, nil
}

// Save écrit l'état complet de la partie dans path, de façon atomique.
// Refusée pendant un combat.
func (g *Game) Save(path string) error {
	if g.InCombat() {
		return inCombat("sauvegarder")
	}
	data, err := json.MarshalIndent(g.snapshot(), "", "  ")
	if err != nil {
		return fmt.Errorf("sauvegarde: encodage JSON: %w", err)
	}
	data = append(data, '\n')
	if err := writeFileAtomic(path, data, 0o644); err != nil {
		return fmt.Errorf("sauvegarde %q: %w", path, err)
	}
	return nil
}

// Load restaure la partie depuis path. En cas d'erreur (fichier absent,
// JSON corrompu, région inconnue…), l'état actuel est conservé intact.
func (g *Game) Load(path string) error {
	if g.InCombat() {
		return inCombat("charger une partie")
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w (%s)", ErrNoSave, path)
	}
	if err != nil {
		return fmt.Errorf("chargement %q: %w", path, err)
	}

	var sf saveFile
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&sf); err != nil {
		return fmt.Errorf("sauvegarde %q corrompue: %w", path, err)
	}
	char, bosses, err := g.validateSave(sf)
	if err != nil {
		return fmt.Errorf("sauvegarde %q invalide: %w", path, err)
	}

	// Tout est valide : on applique d'un bloc.
	g.region = sf.Region
	g.char = char
	g.slain = maps.Clone(sf.Slain)
	if g.slain == nil {
		g.slain = make(map[string]int)
	}
	g.rested = sf.RestedHere
	g.bosses = bosses
	g.enc = nil
	g.corpse = nil
	return nil
}

func (g *Game) snapshot() saveFile {
	c := g.char
	inv := make([]saveWeapon, len(c.Inventory))
	for i, w := range c.Inventory {
		inv[i] = toSaveWeapon(w)
	}
	bosses := make([]string, 0, len(g.bosses))
	for id := range g.bosses {
		bosses = append(bosses, id)
	}
	slices.Sort(bosses)
	return saveFile{
		Version: SaveVersion,
		Region:  g.region,
		Character: saveCharacter{
			Name:        c.Name,
			HP:          c.HP,
			MaxHP:       c.MaxHP,
			Level:       c.Level,
			XP:          c.XP,
			AttackBonus: c.AttackBonus,
			DexMod:      c.DexMod,
			Weapon:      toSaveWeapon(c.Weapon),
			Inventory:   inv,
		},
		Gold:           c.Gold,
		Slain:          maps.Clone(g.slain),
		RestedHere:     g.rested,
		BossesDefeated: bosses,
	}
}

// validateSave vérifie la cohérence d'une sauvegarde et reconstruit le
// personnage et les boss vaincus. Toutes les anomalies sont renvoyées
// ensemble.
func (g *Game) validateSave(sf saveFile) (Character, map[string]bool, error) {
	var errs []error
	if sf.Version < 1 || sf.Version > SaveVersion {
		errs = append(errs, fmt.Errorf("version %d non supportée (attendue: 1 à %d)", sf.Version, SaveVersion))
	}
	if _, ok := g.world.Region(sf.Region); !ok {
		errs = append(errs, fmt.Errorf("la région %q n'existe pas dans ce monde", sf.Region))
	}

	sc := sf.Character
	if strings.TrimSpace(sc.Name) == "" {
		errs = append(errs, errors.New("nom du personnage vide"))
	}
	if sc.Level < 1 {
		errs = append(errs, fmt.Errorf("niveau %d invalide", sc.Level))
	}
	if sc.MaxHP < 1 || sc.HP < 1 || sc.HP > sc.MaxHP {
		errs = append(errs, fmt.Errorf("PV %d/%d invalides", sc.HP, sc.MaxHP))
	}
	if sc.XP < 0 {
		errs = append(errs, fmt.Errorf("XP %d négative", sc.XP))
	}
	if sf.Gold < 0 {
		errs = append(errs, fmt.Errorf("or %d négatif", sf.Gold))
	}
	for slug, kills := range sf.Slain {
		if slug == "" || kills < 1 {
			errs = append(errs, fmt.Errorf("entrée de bestiaire invalide: %q → %d", slug, kills))
		}
	}

	equipped, err := fromSaveWeapon(sc.Weapon)
	if err != nil {
		errs = append(errs, fmt.Errorf("arme équipée: %w", err))
	}
	inventory, err := loadInventory(sc.Inventory, equipped)
	if err != nil {
		errs = append(errs, fmt.Errorf("inventaire: %w", err))
	}

	bosses := make(map[string]bool, len(sf.BossesDefeated))
	for _, id := range sf.BossesDefeated {
		r, ok := g.world.Region(id)
		switch {
		case !ok:
			errs = append(errs, fmt.Errorf("boss vaincu dans une région inconnue: %q", id))
		case r.Boss == nil:
			errs = append(errs, fmt.Errorf("la région %q n'a pas de boss", id))
		default:
			bosses[id] = true
		}
	}

	if err := errors.Join(errs...); err != nil {
		return Character{}, nil, err
	}
	return Character{
		Name:        sc.Name,
		HP:          sc.HP,
		MaxHP:       sc.MaxHP,
		Level:       sc.Level,
		XP:          sc.XP,
		AttackBonus: sc.AttackBonus,
		DexMod:      sc.DexMod,
		Gold:        sf.Gold,
		Weapon:      equipped,
		Inventory:   inventory,
	}, bosses, nil
}

// loadInventory reconstruit l'inventaire. Une sauvegarde v1 n'en a pas :
// il se réduit alors à l'arme équipée. L'arme équipée doit y figurer.
func loadInventory(saved []saveWeapon, equipped Weapon) ([]Weapon, error) {
	if len(saved) == 0 {
		if equipped.Slug == "" {
			return nil, errors.New("aucune arme")
		}
		return []Weapon{equipped}, nil
	}
	inv := make([]Weapon, 0, len(saved))
	seen := make(map[string]bool, len(saved))
	var errs []error
	for _, sw := range saved {
		w, err := fromSaveWeapon(sw)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if seen[w.Slug] {
			errs = append(errs, fmt.Errorf("arme %q en double", w.Slug))
			continue
		}
		seen[w.Slug] = true
		inv = append(inv, w)
	}
	if equipped.Slug != "" && !seen[equipped.Slug] {
		errs = append(errs, fmt.Errorf("l'arme équipée %q n'est pas dans l'inventaire", equipped.Slug))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return inv, nil
}

// writeFileAtomic écrit data dans path + ".tmp", force l'écriture sur
// disque, puis renomme le fichier temporaire. Un crash ou une erreur en cours
// d'écriture ne peut donc jamais laisser une sauvegarde à moitié écrite.
func writeFileAtomic(path string, data []byte, perm fs.FileMode) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return fmt.Errorf("création de %q: %w", tmp, err)
	}

	if err := writeAndClose(f, data); err != nil {
		if rmErr := os.Remove(tmp); rmErr != nil {
			err = errors.Join(err, fmt.Errorf("suppression de %q: %w", tmp, rmErr))
		}
		return err
	}
	if err := renameWithRetry(tmp, path); err != nil {
		if rmErr := os.Remove(tmp); rmErr != nil {
			err = errors.Join(err, fmt.Errorf("suppression de %q: %w", tmp, rmErr))
		}
		return fmt.Errorf("renommage de %q en %q: %w", tmp, path, err)
	}
	return nil
}

// Sous Windows, remplacer un fichier que l'éditeur, l'indexeur ou
// l'antivirus tient ouvert échoue de façon transitoire (« used by another
// process ») : on retente quelques fois avant d'abandonner.
const (
	renameAttempts = 5
	renameBackoff  = 50 * time.Millisecond
)

func renameWithRetry(from, to string) error {
	var err error
	for attempt := 1; attempt <= renameAttempts; attempt++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		if attempt < renameAttempts {
			time.Sleep(time.Duration(attempt) * renameBackoff)
		}
	}
	return fmt.Errorf("après %d tentatives: %w", renameAttempts, err)
}

func writeAndClose(f *os.File, data []byte) error {
	if _, err := f.Write(data); err != nil {
		return errors.Join(fmt.Errorf("écriture de %q: %w", f.Name(), err), closeErr(f))
	}
	if err := f.Sync(); err != nil {
		return errors.Join(fmt.Errorf("synchronisation de %q: %w", f.Name(), err), closeErr(f))
	}
	return closeErr(f)
}

func closeErr(f *os.File) error {
	if err := f.Close(); err != nil {
		return fmt.Errorf("fermeture de %q: %w", f.Name(), err)
	}
	return nil
}
