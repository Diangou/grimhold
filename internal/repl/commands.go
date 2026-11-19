package repl

import (
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/Diangou/grimhold/internal/game"
	"github.com/Diangou/grimhold/internal/open5e"
	"github.com/Diangou/grimhold/internal/world"
)

// newCommands construit le registre des commandes. C'est l'unique source de
// vérité : help est généré à partir de cette map.
func newCommands() map[string]cliCommand {
	cmds := []cliCommand{
		{name: "help", description: "Affiche cette aide", usage: "help", callback: cmdHelp},
		{name: "map", description: "Affiche la carte, les dangers et les sorties", usage: "map", callback: cmdMap},
		{name: "travel", description: "Voyage vers une région voisine (par son id)", usage: "travel <region>", minArgs: 1, maxArgs: 1, callback: cmdTravel},
		{name: "look", description: "Décrit la région actuelle", usage: "look", callback: cmdLook},
		{name: "explore", description: "Fouille la région à la recherche d'un adversaire", usage: "explore", callback: cmdExplore},
		{name: "fight", description: "Joue un round de combat", usage: "fight", callback: cmdFight},
		{name: "flee", description: "Tente de fuir le combat (jet de DEX)", usage: "flee", callback: cmdFlee},
		{name: "rest", description: "Soin complet, une fois par visite de région", usage: "rest", callback: cmdRest},
		{name: "character", description: "Affiche ta fiche de personnage", usage: "character", callback: cmdCharacter},
		{name: "bestiary", description: "Affiche la première page du bestiaire Open5e", usage: "bestiary", callback: cmdBestiary},
		{name: "next", description: "Page suivante du bestiaire", usage: "next", callback: cmdNext},
		{name: "prev", description: "Page précédente du bestiaire", usage: "prev", callback: cmdPrev},
		{name: "inspect", description: "Fiche de stats d'un monstre (slug ou nom)", usage: "inspect <monster>", minArgs: 1, maxArgs: unlimited, callback: cmdInspect},
		{name: "slain", description: "Liste les monstres que tu as tués", usage: "slain", callback: cmdSlain},
		{name: "loot", description: "Fouille le cadavre de ton dernier adversaire", usage: "loot", callback: cmdLoot},
		{name: "inventory", description: "Liste tes armes", usage: "inventory", callback: cmdInventory},
		{name: "equip", description: "Équipe une arme de ton inventaire (slug ou nom)", usage: "equip <weapon>", minArgs: 1, maxArgs: unlimited, callback: cmdEquip},
		{name: "save", description: "Sauvegarde la partie", usage: "save", callback: cmdSave},
		{name: "load", description: "Charge la dernière sauvegarde", usage: "load", callback: cmdLoad},
		{name: "quit", description: "Quitte le jeu", usage: "quit", callback: cmdQuit},
		{name: "exit", description: "Quitte le jeu (alias de quit)", usage: "exit", callback: cmdQuit},
	}
	m := make(map[string]cliCommand, len(cmds))
	for _, c := range cmds {
		if _, dup := m[c.name]; dup {
			panic(fmt.Sprintf("repl: commande %q enregistrée deux fois", c.name))
		}
		m[c.name] = c
	}
	return m
}

// ---------------------------------------------------------------- aide

func cmdHelp(s *Session, _ []string) error {
	names := make([]string, 0, len(s.commands))
	for name := range s.commands {
		names = append(names, name)
	}
	slices.Sort(names)

	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 3, ' ', 0)
	for _, name := range names {
		c := s.commands[name]
		if _, err := fmt.Fprintf(tw, "  %s\t%s\n", c.usage, c.description); err != nil {
			return fmt.Errorf("mise en forme de l'aide: %w", err)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("mise en forme de l'aide: %w", err)
	}
	s.out.Println("Commandes disponibles :")
	s.out.Print(buf.String())
	return nil
}

func cmdQuit(s *Session, _ []string) error {
	s.out.Println(farewell)
	return errQuit
}

// ---------------------------------------------------------------- monde

func cmdMap(s *Session, _ []string) error {
	current := s.game.Region().ID
	var buf bytes.Buffer
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	for _, r := range s.game.World().Regions() {
		marker := " "
		if r.ID == current {
			marker = "*"
		}
		if _, err := fmt.Fprintf(tw, "  %s %s\t%s\tdanger %d\t→ %s\t%s\n",
			marker, r.ID, r.Name, r.Danger, strings.Join(r.Exits, ", "), s.regionTags(r)); err != nil {
			return fmt.Errorf("mise en forme de la carte: %w", err)
		}
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("mise en forme de la carte: %w", err)
	}
	s.out.Println("Carte des terres maudites (* = ta position) :")
	// La dernière colonne (sceau/boss) est souvent vide : on retire le
	// remplissage laissé par tabwriter en fin de ligne.
	for _, line := range strings.Split(strings.TrimRight(buf.String(), "\n"), "\n") {
		s.out.Println(strings.TrimRight(line, " "))
	}
	return nil
}

// regionTags résume le sceau de niveau et le boss d'une région.
func (s *Session) regionTags(r world.Region) string {
	var tags []string
	if r.MinLevel > 1 {
		tags = append(tags, fmt.Sprintf("[niveau %d requis]", r.MinLevel))
	}
	if r.Boss != nil {
		state := "boss"
		if s.game.BossDefeated(r.ID) {
			state = "vaincu"
		}
		tags = append(tags, fmt.Sprintf("[%s : %s]", state, r.Boss.Title))
	}
	return strings.Join(tags, " ")
}

func cmdTravel(s *Session, args []string) error {
	dest, err := s.game.Travel(strings.ToLower(args[0]))
	if err != nil {
		return err
	}
	s.out.Printf("Tu prends la route vers %s...\n\n", dest.Name)
	s.describeRegion(dest)
	return nil
}

func cmdLook(s *Session, _ []string) error {
	s.describeRegion(s.game.Region())
	if enc, ok := s.game.Encounter(); ok {
		f := enc.Foe
		s.out.Printf("\n%s te fait face (%d/%d PV). Tape fight ou flee.\n", f.Name, f.HP, f.MaxHP)
	}
	return nil
}

func (s *Session) describeRegion(r world.Region) {
	s.out.Printf("== %s ==\n%s\n", r.Name, r.Description)
	s.out.Printf("Danger : %d (%s)\n", r.Danger, world.DangerHint(r.Danger))
	exits := make([]string, 0, len(r.Exits))
	for _, id := range r.Exits {
		if dst, ok := s.game.World().Region(id); ok {
			exits = append(exits, fmt.Sprintf("%s (%s)", id, dst.Name))
		}
	}
	s.out.Printf("Sorties : %s\n", strings.Join(exits, ", "))
	for _, id := range r.Exits {
		if dst, ok := s.game.World().Region(id); ok && dst.MinLevel > s.game.Character().Level {
			s.out.Printf("Un sceau de cendres scelle la route vers %s (niveau %d requis).\n", dst.Name, dst.MinLevel)
		}
	}
	if r.Boss != nil {
		if s.game.BossDefeated(r.ID) {
			s.out.Printf("Les restes de %s gisent ici. La malédiction est brisée.\n", r.Boss.Title)
		} else {
			s.out.Printf("%s attend ici. Tape explore si tu l'oses.\n", r.Boss.Title)
		}
	}
	if s.game.HasCorpse() {
		s.out.Println("Un cadavre encore chaud attend d'être fouillé (loot).")
	}
	if s.game.CanRest() {
		s.out.Println("Un coin semble assez sûr pour se reposer.")
	}
}

// ---------------------------------------------------------------- combat

func cmdExplore(s *Session, _ []string) error {
	fetch := func(slug string) (open5e.Monster, error) {
		return s.client.GetMonster(s.ctx, slug)
	}
	foe, err := s.game.Explore(fetch)
	if errors.Is(err, game.ErrNothingHere) {
		s.out.Printf("Tu fouilles les environs. Rien ne bouge... %v.\n", err)
		return nil
	}
	if err != nil {
		return err
	}
	if foe.Boss {
		s.out.Printf("Le silence se brise. %s se lève de son trône (%s, CR %s) !\n", foe.Name, foe.Species, foe.CRLabel)
		s.out.Printf("  CA %d · PV %d · %d attaques par round : %s\n", foe.AC, foe.HP, foe.AttacksPerRound, attackNames(foe.Attacks))
		s.out.Println("Tape fight pour l'affronter ou flee pour fuir.")
		return nil
	}
	s.out.Printf("Quelque chose remue dans l'ombre... %s (CR %s) surgit !\n", foe.Name, foe.CRLabel)
	s.out.Printf("  CA %d · PV %d · attaques : %s\n", foe.AC, foe.HP, attackNames(foe.Attacks))
	s.out.Println("Tape fight pour combattre ou flee pour fuir.")
	return nil
}

func attackNames(atks []game.Attack) string {
	names := make([]string, len(atks))
	for i, a := range atks {
		names[i] = fmt.Sprintf("%s (%+d, %s)", a.Name, a.Bonus, a.Damage)
	}
	return strings.Join(names, ", ")
}

func cmdFight(s *Session, _ []string) error {
	rep, err := s.game.Fight()
	if err != nil {
		return err
	}
	s.out.Printf("— Round %d —\n", rep.Round)
	s.printAttack("Tu attaques", rep.Player)
	for _, fa := range rep.FoeAttacks {
		s.printAttack(rep.FoeName+" riposte", fa)
	}
	s.out.Printf("  PV : toi %d/%d · %s %d/%d\n", rep.PlayerHP, rep.PlayerMaxHP, rep.FoeName, rep.FoeHP, rep.FoeMaxHP)

	switch rep.Outcome {
	case game.OutcomeVictory:
		s.printVictory(*rep.Victory)
	case game.OutcomeDefeat:
		s.printDefeat(rep.RespawnAt)
	case game.OutcomeOngoing:
		s.out.Println("Le combat continue : fight ou flee ?")
	}
	return nil
}

func (s *Session) printAttack(who string, a game.AttackResult) {
	roll := fmt.Sprintf("d20 %d %+d = %d contre CA %d", a.Natural, a.Bonus, a.Total, a.TargetAC)
	switch {
	case a.Fumble:
		s.out.Printf("  %s (%s) : %s → 1 naturel, échec critique !\n", who, a.Weapon, roll)
	case a.Critical:
		s.out.Printf("  %s (%s) : %s → 20 naturel, CRITIQUE ! %s\n", who, a.Weapon, roll, damage(a.Damage))
	case a.Hit:
		s.out.Printf("  %s (%s) : %s → touché, %s\n", who, a.Weapon, roll, damage(a.Damage))
	default:
		s.out.Printf("  %s (%s) : %s → raté\n", who, a.Weapon, roll)
	}
}

func (s *Session) printVictory(v game.Victory) {
	if v.Boss {
		s.out.Printf("%s s'effondre dans un fracas d'os et de cendres. La malédiction de Grimhold est brisée !\n", v.FoeName)
		s.out.Printf("Victoire légendaire ! +%d XP, +%d or.\n", v.XP, v.Gold)
	} else {
		s.out.Printf("%s s'effondre. Victoire ! +%d XP, +%d or. (%s abattus : %d)\n",
			v.FoeName, v.XP, v.Gold, v.FoeName, v.Kills)
	}
	for _, up := range v.LevelUps {
		s.out.Printf("*** NIVEAU %d ! PV max %d (soin complet), bonus d'attaque %+d ***\n",
			up.Level, up.MaxHP, up.AttackBonus)
	}
	s.out.Println("Tape loot pour fouiller le cadavre.")
}

// taunts sont affichés tour à tour à chaque mort.
var taunts = []string{
	"Les corbeaux se disputent déjà ta légende. Elle est courte.",
	"Même les rats du marais racontent ta chute en riant.",
	"Ton épée est restée là-bas. Ta dignité aussi.",
	"Les morts t'ont renvoyé : ils ne veulent pas de toi non plus.",
}

func (s *Session) printDefeat(respawn string) {
	taunt := taunts[s.deaths%len(taunts)]
	s.deaths++
	s.out.Println("Tout devient noir...")
	s.out.Printf("Tu te réveilles à %s avec 1 PV. %s\n", respawn, taunt)
}

func cmdFlee(s *Session, _ []string) error {
	rep, err := s.game.Flee()
	if err != nil {
		return err
	}
	s.out.Printf("Jet de fuite : d20 %d %+d = %d contre DD %d → %s\n",
		rep.Natural, rep.Mod, rep.Total, rep.DC, map[bool]string{true: "réussi", false: "raté"}[rep.Success])
	if rep.FreeAttack != nil {
		s.printAttack(rep.FoeName+" profite de ta fuite", *rep.FreeAttack)
	}
	switch rep.Outcome {
	case game.OutcomeDefeat:
		s.printDefeat(rep.RespawnAt)
	default:
		s.out.Printf("Tu t'échappes, le cœur battant (%d PV).\n", rep.PlayerHP)
	}
	return nil
}

func cmdRest(s *Session, _ []string) error {
	healed, err := s.game.Rest()
	if err != nil {
		return err
	}
	c := s.game.Character()
	s.out.Printf("Tu dresses un camp de fortune et dors d'un sommeil agité. +%d PV (%d/%d).\n", healed, c.HP, c.MaxHP)
	return nil
}

// ---------------------------------------------------------------- personnage

func cmdCharacter(s *Session, _ []string) error {
	c := s.game.Character()
	r := s.game.Region()
	rest := "repos disponible"
	if !s.game.CanRest() {
		rest = "déjà reposé ici"
	}
	s.out.Printf("%s — niveau %d\n", c.Name, c.Level)
	s.out.Printf("  PV       : %d/%d\n", c.HP, c.MaxHP)
	s.out.Printf("  XP       : %d/%d (prochain niveau)\n", c.XP, c.NextLevelXP())
	s.out.Printf("  Attaque  : %+d avec %s (%s)\n", c.AttackBonus, c.Weapon.Name, c.Weapon.Damage)
	s.out.Printf("  Armes    : %d (inventory pour la liste)\n", len(c.Inventory))
	s.out.Printf("  CA       : %d\n", c.ArmorClass())
	s.out.Printf("  DEX      : %+d\n", c.DexMod)
	s.out.Printf("  Or       : %d\n", c.Gold)
	s.out.Printf("  Position : %s (%s)\n", r.Name, rest)
	return nil
}

func cmdSlain(s *Session, _ []string) error {
	slain := s.game.Slain()
	if len(slain) == 0 {
		s.out.Println("Ton tableau de chasse est vide. Pour l'instant.")
		return nil
	}
	slugs := make([]string, 0, len(slain))
	total := 0
	for slug, n := range slain {
		slugs = append(slugs, slug)
		total += n
	}
	slices.Sort(slugs)
	s.out.Println("Tableau de chasse :")
	for _, slug := range slugs {
		s.out.Printf("  %-20s x%d\n", slug, slain[slug])
	}
	s.out.Printf("Total : %d victime(s).\n", total)
	return nil
}

// ---------------------------------------------------------------- butin

func cmdLoot(s *Session, _ []string) error {
	// Vérifications avant l'appel réseau : inutile de télécharger
	// l'armurerie s'il n'y a rien à fouiller.
	if s.game.InCombat() {
		return fmt.Errorf("tu ne peux pas fouiller un cadavre en plein combat (%w)", game.ErrInCombat)
	}
	if !s.game.HasCorpse() {
		return game.ErrNothingToLoot
	}
	weapons, err := s.client.ListWeapons(s.ctx)
	if err != nil {
		return fmt.Errorf("fouille: %w", err)
	}
	rep, err := s.game.Loot(game.ArmoryFromAPI(weapons))
	if err != nil {
		return err
	}
	switch {
	case rep.Weapon == nil:
		s.out.Printf("Tu fouilles %s : rien que des haillons et des os.\n", rep.FoeName)
	case rep.New:
		w := rep.Weapon
		s.out.Printf("Tu fouilles %s et trouves : %s (%s, %.1f dégâts en moyenne) !\n", rep.FoeName, w.Name, w.Damage, w.Damage.Average())
		s.out.Printf("Tape equip %s pour t'en servir.\n", w.Slug)
	default:
		s.out.Printf("Tu trouves : %s, mais tu l'as déjà. Revendu pour %d or.\n", rep.Weapon.Name, rep.Gold)
	}
	return nil
}

func cmdInventory(s *Session, _ []string) error {
	c := s.game.Character()
	s.out.Println("Tes armes :")
	for _, w := range c.Inventory {
		marker := " "
		if w.Slug == c.Weapon.Slug {
			marker = "*"
		}
		s.out.Printf("  %s %-16s %-6s moy. %4.1f  (%s)\n", marker, w.Name, w.Damage, w.Damage.Average(), w.Slug)
	}
	s.out.Println("(* = équipée ; equip <weapon> pour changer)")
	return nil
}

func cmdEquip(s *Session, args []string) error {
	w, err := s.game.Equip(strings.Join(args, " "))
	if err != nil {
		return err
	}
	s.out.Printf("Tu empoignes %s (%s).\n", w.Name, w.Damage)
	return nil
}

// ---------------------------------------------------------------- bestiaire

func cmdBestiary(s *Session, _ []string) error {
	return s.showPage(s.client.FirstPageURL(), 1)
}

func cmdNext(s *Session, _ []string) error {
	if s.page == nil {
		return errors.New("aucune page ouverte : tape d'abord bestiary")
	}
	if s.page.Next == nil {
		return errors.New("tu es déjà sur la dernière page du bestiaire")
	}
	return s.showPage(*s.page.Next, s.pageNum+1)
}

func cmdPrev(s *Session, _ []string) error {
	if s.page == nil {
		return errors.New("aucune page ouverte : tape d'abord bestiary")
	}
	if s.page.Previous == nil {
		return errors.New("tu es déjà sur la première page du bestiaire")
	}
	return s.showPage(*s.page.Previous, s.pageNum-1)
}

func (s *Session) showPage(url string, num int) error {
	list, err := s.client.ListMonsters(s.ctx, url)
	if err != nil {
		return err
	}
	s.page, s.pageNum = &list, num

	pages := 1
	if size := s.client.PageSize(); size > 0 && list.Count > 0 {
		pages = (list.Count + size - 1) / size
	}
	s.out.Printf("Bestiaire — page %d/%d (%d monstres)\n", num, pages, list.Count)
	for _, m := range list.Results {
		s.out.Printf("  %-28s CR %-4s (%s)\n", m.Name, m.ChallengeRating, m.Slug)
	}
	var hints []string
	if list.Previous != nil {
		hints = append(hints, "prev")
	}
	if list.Next != nil {
		hints = append(hints, "next")
	}
	if len(hints) > 0 {
		s.out.Printf("(%s pour naviguer, inspect <monster> pour une fiche)\n", strings.Join(hints, " / "))
	}
	return nil
}

func cmdInspect(s *Session, args []string) error {
	slug := open5e.Slugify(strings.Join(args, " "))
	if slug == "" {
		return fmt.Errorf("nom de monstre invalide : %q", strings.Join(args, " "))
	}
	m, err := s.client.GetMonster(s.ctx, slug)
	if errors.Is(err, open5e.ErrNotFound) {
		return fmt.Errorf("monstre inconnu : %q (consulte bestiary pour les slugs valides)", slug)
	}
	if err != nil {
		return err
	}
	s.printMonster(m)
	return nil
}

func (s *Session) printMonster(m open5e.Monster) {
	kind := strings.ToLower(m.Type)
	if m.Subtype != "" {
		kind += " (" + m.Subtype + ")"
	}
	s.out.Printf("%s  [%s]\n", m.Name, m.Slug)
	s.out.Printf("  %s %s, %s\n", m.Size, kind, m.Alignment)

	ac := fmt.Sprintf("CA %d", m.ArmorClass)
	if m.ArmorDesc != "" {
		ac += " (" + m.ArmorDesc + ")"
	}
	cr := fmt.Sprintf("CR %s", m.ChallengeRating)
	if v, err := m.ChallengeRating.Float(); err == nil {
		cr += fmt.Sprintf(" (%d XP)", game.XPForCR(v))
	}
	s.out.Printf("  %s · PV %d (%s) · %s\n", ac, m.HitPoints, m.HitDice, cr)

	stats := []struct {
		label string
		score int
	}{
		{"FOR", m.Strength}, {"DEX", m.Dexterity}, {"CON", m.Constitution},
		{"INT", m.Intelligence}, {"SAG", m.Wisdom}, {"CHA", m.Charisma},
	}
	parts := make([]string, len(stats))
	for i, st := range stats {
		parts[i] = fmt.Sprintf("%s %d (%+d)", st.label, st.score, game.AbilityMod(st.score))
	}
	s.out.Printf("  %s\n", strings.Join(parts, "  "))
	if m.Senses != "" {
		s.out.Printf("  Sens : %s\n", m.Senses)
	}
	if m.Languages != "" {
		s.out.Printf("  Langues : %s\n", m.Languages)
	}
	if len(m.Actions) == 0 {
		return
	}
	s.out.Println("  Actions :")
	for _, a := range m.Actions {
		if a.AttackBonus != nil && a.DamageDice != "" {
			dmg := a.DamageDice
			if a.DamageBonus != nil && *a.DamageBonus != 0 {
				dmg += fmt.Sprintf("%+d", *a.DamageBonus)
			}
			s.out.Printf("    - %s : %+d pour toucher, %s dégâts\n", a.Name, *a.AttackBonus, dmg)
			continue
		}
		s.out.Printf("    - %s : %s\n", a.Name, truncate(a.Desc, 110))
	}
}

func truncate(s string, n int) string {
	r := []rune(strings.Join(strings.Fields(s), " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n-1]) + "…"
}

// ---------------------------------------------------------------- save/load

func cmdSave(s *Session, _ []string) error {
	if err := s.game.Save(s.savePath); err != nil {
		return err
	}
	s.out.Printf("Partie sauvegardée dans %s.\n", s.savePath)
	return nil
}

func cmdLoad(s *Session, _ []string) error {
	err := s.game.Load(s.savePath)
	if errors.Is(err, game.ErrNoSave) {
		s.out.Println("Aucune sauvegarde trouvée. Une nouvelle aventure commence.")
		return nil
	}
	if err != nil {
		return fmt.Errorf("%w\nL'état actuel de la partie est conservé", err)
	}
	c := s.game.Character()
	s.out.Printf("Partie chargée : %s, niveau %d, %d/%d PV, à %s.\n",
		c.Name, c.Level, c.HP, c.MaxHP, s.game.Region().Name)
	return nil
}

// damage accorde « dégât(s) » selon la quantité.
func damage(n int) string {
	if n > 1 {
		return fmt.Sprintf("%d dégâts", n)
	}
	return fmt.Sprintf("%d dégât", n)
}
