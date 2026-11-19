package open5e

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// MonsterList est une page de résultats de /v1/monsters/.
type MonsterList struct {
	Count    int       `json:"count"`
	Next     *string   `json:"next"`
	Previous *string   `json:"previous"`
	Results  []Monster `json:"results"`
}

// Monster est la fiche d'un monstre telle que renvoyée par Open5e (v1).
// Seuls les champs utilisés par le jeu sont décodés.
type Monster struct {
	Name            string          `json:"name"`
	Slug            string          `json:"slug"`
	Size            string          `json:"size"`
	Type            string          `json:"type"`
	Subtype         string          `json:"subtype"`
	Alignment       string          `json:"alignment"`
	ArmorClass      int             `json:"armor_class"`
	ArmorDesc       string          `json:"armor_desc"`
	HitPoints       int             `json:"hit_points"`
	HitDice         string          `json:"hit_dice"`
	ChallengeRating ChallengeRating `json:"challenge_rating"`
	Strength        int             `json:"strength"`
	Dexterity       int             `json:"dexterity"`
	Constitution    int             `json:"constitution"`
	Intelligence    int             `json:"intelligence"`
	Wisdom          int             `json:"wisdom"`
	Charisma        int             `json:"charisma"`
	Senses          string          `json:"senses"`
	Languages       string          `json:"languages"`
	Actions         []Action        `json:"actions"`
	DocumentSlug    string          `json:"document__slug"`
}

// Action est une action de monstre. AttackBonus et DamageBonus sont des
// pointeurs car l'API renvoie null pour les actions qui ne sont pas des attaques
// (et 0 est un bonus légitime). DamageDice est vide dans ce cas.
type Action struct {
	Name        string `json:"name"`
	Desc        string `json:"desc"`
	AttackBonus *int   `json:"attack_bonus"`
	DamageDice  string `json:"damage_dice"`
	DamageBonus *int   `json:"damage_bonus"`
}

// ChallengeRating est le facteur de puissance, transmis par l'API sous forme
// de chaîne ("1/4", "2"…). Le type accepte aussi un nombre JSON par robustesse.
type ChallengeRating string

// UnmarshalJSON accepte "1/4", "2" ou 2.
func (c *ChallengeRating) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*c = ""
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*c = ChallengeRating(s)
		return nil
	}
	var f float64
	if err := json.Unmarshal(data, &f); err != nil {
		return fmt.Errorf("challenge_rating: ni chaîne ni nombre: %s", data)
	}
	*c = ChallengeRating(strconv.FormatFloat(f, 'f', -1, 64))
	return nil
}

// Float convertit le CR en nombre ("1/4" → 0.25).
func (c ChallengeRating) Float() (float64, error) {
	return ParseCR(string(c))
}

// ParseCR convertit un facteur de puissance texte en float64.
// Formats acceptés : entier ("3"), décimal ("0.5") ou fraction ("1/8").
func ParseCR(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("facteur de puissance vide")
	}
	var v float64
	if num, den, isFrac := strings.Cut(s, "/"); isFrac {
		n, err := strconv.ParseFloat(strings.TrimSpace(num), 64)
		if err != nil {
			return 0, fmt.Errorf("facteur de puissance %q: numérateur: %w", s, err)
		}
		d, err := strconv.ParseFloat(strings.TrimSpace(den), 64)
		if err != nil {
			return 0, fmt.Errorf("facteur de puissance %q: dénominateur: %w", s, err)
		}
		if d == 0 {
			return 0, fmt.Errorf("facteur de puissance %q: division par zéro", s)
		}
		v = n / d
	} else {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return 0, fmt.Errorf("facteur de puissance %q: %w", s, err)
		}
		v = f
	}
	if v < 0 {
		return 0, fmt.Errorf("facteur de puissance %q: négatif", s)
	}
	return v, nil
}

// WeaponList est une page de résultats de /v1/weapons/.
type WeaponList struct {
	Count    int      `json:"count"`
	Next     *string  `json:"next"`
	Previous *string  `json:"previous"`
	Results  []Weapon `json:"results"`
}

// Weapon est une arme du SRD. DamageDice peut ne pas être une expression de
// dés (ex. "1" pour la sarbacane, "0" pour le filet).
type Weapon struct {
	Name       string   `json:"name"`
	Slug       string   `json:"slug"`
	Category   string   `json:"category"`
	Cost       string   `json:"cost"`
	DamageDice string   `json:"damage_dice"`
	DamageType string   `json:"damage_type"`
	Weight     string   `json:"weight"`
	Properties []string `json:"properties"`
}
