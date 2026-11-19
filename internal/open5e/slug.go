package open5e

import "strings"

// Slugify convertit un nom libre en slug Open5e : "Giant Spider" → "giant-spider".
// Les lettres accentuées courantes perdent leur accent ("Épée" → "epee") ; les
// autres caractères hors [a-z0-9] deviennent des tirets, sans tirets multiples
// ni en bordure.
func Slugify(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if plain, ok := accents[r]; ok {
			r = plain
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash && b.Len() > 0:
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.TrimSuffix(b.String(), "-")
}

// accents associe les lettres accentuées minuscules à leur forme de base.
var accents = map[rune]rune{
	'à': 'a', 'â': 'a', 'ä': 'a', 'á': 'a', 'ç': 'c',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'î': 'i', 'ï': 'i', 'í': 'i', 'ô': 'o', 'ö': 'o', 'ó': 'o',
	'ù': 'u', 'û': 'u', 'ü': 'u', 'ú': 'u', 'ÿ': 'y', 'ñ': 'n',
}

// ValidSlug indique si s est un slug bien formé (sûr à placer dans une URL).
func ValidSlug(s string) bool {
	return s != "" && Slugify(s) == s
}
