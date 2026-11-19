package game

import "sort"

// xpByCR est la table officielle D&D 5e (Monster Manual / SRD) de l'XP
// accordée par facteur de puissance. Un CR 0 rapporte 10 XP (créature
// pouvant attaquer).
var xpByCR = map[float64]int{
	0: 10, 0.125: 25, 0.25: 50, 0.5: 100,
	1: 200, 2: 450, 3: 700, 4: 1100, 5: 1800,
	6: 2300, 7: 2900, 8: 3900, 9: 5000, 10: 5900,
	11: 7200, 12: 8400, 13: 10000, 14: 11500, 15: 13000,
	16: 15000, 17: 18000, 18: 20000, 19: 22000, 20: 25000,
	21: 33000, 22: 41000, 23: 50000, 24: 62000, 25: 75000,
	26: 90000, 27: 105000, 28: 120000, 29: 135000, 30: 155000,
}

// XPForCR renvoie l'XP d'un monstre de CR donné. Un CR absent de la table
// prend la valeur du palier inférieur le plus proche.
func XPForCR(cr float64) int {
	if xp, ok := xpByCR[cr]; ok {
		return xp
	}
	keys := make([]float64, 0, len(xpByCR))
	for k := range xpByCR {
		keys = append(keys, k)
	}
	sort.Float64s(keys)
	best := keys[0]
	for _, k := range keys {
		if k <= cr {
			best = k
		}
	}
	return xpByCR[best]
}
