// Package world charge, valide et interroge la carte du jeu (world.json).
package world

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
)

// MaxDanger est le niveau de danger maximal accepté pour une région.
const MaxDanger = 5

// Erreurs sentinelles exposées pour que l'appelant puisse les tester avec errors.Is.
var (
	ErrUnknownRegion = errors.New("région inconnue")
	ErrNoExit        = errors.New("aucun passage")
)

// Region est une zone de la carte telle que décrite dans world.json.
type Region struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Danger      int      `json:"danger"`
	Foes        []string `json:"foes"`
	Exits       []string `json:"exits"`
	// MinLevel est le niveau requis pour entrer (0 = aucun). Optionnel.
	MinLevel int `json:"min_level,omitempty"`
	// Boss est l'adversaire unique de la région. Optionnel.
	Boss *Boss `json:"boss,omitempty"`
}

// Boss décrit un adversaire unique : un monstre Open5e (Slug) renommé
// (Title), dont les PV peuvent être ajustés pour l'équilibrage.
type Boss struct {
	Slug      string `json:"slug"`
	Title     string `json:"title"`
	HitPoints int    `json:"hit_points,omitempty"` // 0 = PV de la fiche Open5e
}

// World est une carte validée. Ses champs ne sont pas exportés : une fois
// construit par Parse ou Load, un World est garanti cohérent.
type World struct {
	start   string
	regions []Region
	index   map[string]int
}

// fileFormat reflète exactement la structure de world.json.
type fileFormat struct {
	Start   string   `json:"start"`
	Regions []Region `json:"regions"`
}

// Load lit et valide le fichier de monde situé à path.
func Load(path string) (*World, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("lecture du monde %q: %w", path, err)
	}
	w, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("monde %q: %w", path, err)
	}
	return w, nil
}

// Parse décode et valide un monde à partir de son JSON.
func Parse(data []byte) (*World, error) {
	var f fileFormat
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("JSON invalide: %w", err)
	}
	if err := validate(f); err != nil {
		return nil, fmt.Errorf("monde invalide: %w", err)
	}

	w := &World{
		start:   f.Start,
		regions: make([]Region, len(f.Regions)),
		index:   make(map[string]int, len(f.Regions)),
	}
	for i, r := range f.Regions {
		w.regions[i] = cloneRegion(r)
		w.index[r.ID] = i
	}
	return w, nil
}

// validate vérifie la cohérence du monde et renvoie toutes les anomalies
// trouvées d'un coup (errors.Join), pour éviter les allers-retours.
func validate(f fileFormat) error {
	var errs []error
	if len(f.Regions) == 0 {
		return errors.New("aucune région définie")
	}

	ids := make(map[string]bool, len(f.Regions))
	for i, r := range f.Regions {
		switch {
		case strings.TrimSpace(r.ID) == "":
			errs = append(errs, fmt.Errorf("région n°%d: id vide", i+1))
		case ids[r.ID]:
			errs = append(errs, fmt.Errorf("id de région en double: %q", r.ID))
		default:
			ids[r.ID] = true
		}
	}

	if f.Start == "" {
		errs = append(errs, errors.New(`champ "start" manquant`))
	} else if !ids[f.Start] {
		errs = append(errs, fmt.Errorf("région de départ %q introuvable", f.Start))
	}

	for _, r := range f.Regions {
		if strings.TrimSpace(r.Name) == "" {
			errs = append(errs, fmt.Errorf("région %q: nom vide", r.ID))
		}
		if r.Danger < 0 || r.Danger > MaxDanger {
			errs = append(errs, fmt.Errorf("région %q: danger %d hors de [0, %d]", r.ID, r.Danger, MaxDanger))
		}
		if r.MinLevel < 0 {
			errs = append(errs, fmt.Errorf("région %q: min_level %d négatif", r.ID, r.MinLevel))
		}
		if r.ID == f.Start && r.MinLevel > 1 {
			errs = append(errs, fmt.Errorf("région de départ %q: min_level %d la rendrait inaccessible", r.ID, r.MinLevel))
		}
		if b := r.Boss; b != nil {
			if strings.TrimSpace(b.Slug) == "" {
				errs = append(errs, fmt.Errorf("région %q: boss sans slug", r.ID))
			}
			if strings.TrimSpace(b.Title) == "" {
				errs = append(errs, fmt.Errorf("région %q: boss sans titre", r.ID))
			}
			if b.HitPoints < 0 {
				errs = append(errs, fmt.Errorf("région %q: PV du boss %d négatifs", r.ID, b.HitPoints))
			}
		}
		for _, foe := range r.Foes {
			if strings.TrimSpace(foe) == "" {
				errs = append(errs, fmt.Errorf("région %q: ennemi vide dans foes", r.ID))
			}
		}
		seen := make(map[string]bool, len(r.Exits))
		for _, exit := range r.Exits {
			switch {
			case exit == r.ID:
				errs = append(errs, fmt.Errorf("région %q: une sortie pointe sur elle-même", r.ID))
			case !ids[exit]:
				errs = append(errs, fmt.Errorf("région %q: la sortie %q ne mène à aucune région", r.ID, exit))
			case seen[exit]:
				errs = append(errs, fmt.Errorf("région %q: sortie %q en double", r.ID, exit))
			}
			seen[exit] = true
		}
	}
	return errors.Join(errs...)
}

// Start renvoie l'id de la région de départ.
func (w *World) Start() string { return w.start }

// Region renvoie la région d'id donné.
func (w *World) Region(id string) (Region, bool) {
	i, ok := w.index[id]
	if !ok {
		return Region{}, false
	}
	return cloneRegion(w.regions[i]), true
}

// Regions renvoie toutes les régions, dans l'ordre du fichier.
func (w *World) Regions() []Region {
	out := make([]Region, len(w.regions))
	for i, r := range w.regions {
		out[i] = cloneRegion(r)
	}
	return out
}

// CheckTravel vérifie qu'on peut aller de from à to par une sortie directe.
// L'erreur renvoyée liste les sorties possibles.
func (w *World) CheckTravel(from, to string) error {
	src, ok := w.Region(from)
	if !ok {
		return fmt.Errorf("%w: %q", ErrUnknownRegion, from)
	}
	if slices.Contains(src.Exits, to) {
		return nil
	}
	if _, exists := w.Region(to); !exists {
		return fmt.Errorf("%w: %q (sorties possibles: %s)", ErrUnknownRegion, to, strings.Join(src.Exits, ", "))
	}
	return fmt.Errorf("%w entre %s et %q (sorties possibles: %s)", ErrNoExit, src.Name, to, strings.Join(src.Exits, ", "))
}

// DangerHint traduit un niveau de danger en ambiance.
func DangerHint(danger int) string {
	switch {
	case danger <= 0:
		return "un refuge, pour l'instant"
	case danger == 1:
		return "calme inquiétant"
	case danger == 2:
		return "des yeux vous suivent depuis l'ombre"
	case danger == 3:
		return "l'air pue la mort"
	case danger == 4:
		return "même les corbeaux évitent cet endroit"
	default:
		return "chaque pas ici est une prière"
	}
}

func cloneRegion(r Region) Region {
	r.Foes = slices.Clone(r.Foes)
	r.Exits = slices.Clone(r.Exits)
	if r.Boss != nil {
		b := *r.Boss
		r.Boss = &b
	}
	return r
}
