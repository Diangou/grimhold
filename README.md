# Grimhold

Dungeon crawler textuel en Go, joué dans le terminal. Tu explores des régions maudites, tu tombes sur des rencontres aléatoires, tu combats aux dés, tu fouilles les cadavres pour trouver de meilleures armes et tu tiens le compte de tes victimes. Au sommet de la Black Spire, derrière un sceau de niveau, t'attend **The Hollow King**. Les monstres et les armes viennent en direct de l'API [Open5e](https://open5e.com) (SRD de D&D 5e, sans clé). La carte est décrite dans `world.json`.

Bibliothèque standard uniquement, Go 1.22+.

## Compiler et lancer

```sh
go build -o grimhold ./cmd/grimhold
./grimhold                    # nouvelle partie
./grimhold --continue         # reprend directement save.json
```

| Flag | Défaut | Rôle |
|---|---|---|
| `--continue` | `false` | charge la sauvegarde au lancement |
| `--name` | `Errant` | nom du personnage (nouvelle partie) |
| `--world` | `world.json` | fichier de monde |
| `--save` | `save.json` | fichier de sauvegarde |
| `--seed` | `0` (aléatoire) | graine des dés, pour rejouer une partie à l'identique |
| `--api` | `https://api.open5e.com/v1/` | racine de l'API Open5e (serveur de test possible) |
| `--cache-ttl` | `5m` | durée de vie du cache HTTP |

Quand l'entrée est un pipe, chaque commande lue est réaffichée après le prompt, ce qui garde la transcription lisible.

## Sauvegarder / charger

- `save` écrit tout l'état dans `save.json`, en JSON indenté : région, personnage, arme équipée, inventaire, or, victimes, repos et boss vaincus. Le fichier porte un champ `version` (actuellement 2). L'écriture est atomique : `save.json.tmp`, `Sync`, puis `os.Rename`. Sous Windows, le renommage est retenté quelques fois si un autre processus (éditeur, antivirus) tient le fichier ouvert.
- `save` est refusée pendant un combat. Rien n'est jamais écrit automatiquement, ni pendant un combat ni à la mort : une mort ne peut pas corrompre une sauvegarde.
- `load` (ou `--continue`) : si le fichier est absent, le message « aucune sauvegarde trouvée » s'affiche. Si le JSON est corrompu ou incohérent (région inconnue, arme équipée absente de l'inventaire, boss inexistant…), une erreur claire liste tous les problèmes et l'état actuel est conservé. `load` est aussi refusé en combat.
- Les sauvegardes **v1** (avant le loot) se chargent toujours : l'inventaire est reconstruit à partir de l'arme équipée.

## Commandes

```
  bestiary            Affiche la première page du bestiaire Open5e
  character           Affiche ta fiche de personnage
  equip <weapon>      Équipe une arme de ton inventaire (slug ou nom)
  exit                Quitte le jeu (alias de quit)
  explore             Fouille la région à la recherche d'un adversaire
  fight               Joue un round de combat
  flee                Tente de fuir le combat (jet de DEX)
  help                Affiche cette aide
  inspect <monster>   Fiche de stats d'un monstre (slug ou nom)
  inventory           Liste tes armes
  load                Charge la dernière sauvegarde
  look                Décrit la région actuelle
  loot                Fouille le cadavre de ton dernier adversaire
  map                 Affiche la carte, les dangers et les sorties
  next                Page suivante du bestiaire
  prev                Page précédente du bestiaire
  quit                Quitte le jeu
  rest                Soin complet, une fois par visite de région
  save                Sauvegarde la partie
  slain               Liste les monstres que tu as tués
  travel <region>     Voyage vers une région voisine (par son id)
```

`help` est généré à partir du registre des commandes (`internal/repl/commands.go`) : il n'est jamais écrit à la main. Ctrl+D (EOF) quitte aussi proprement.

## Architecture

```
cmd/grimhold/main.go   flags, construction des dépendances, lancement du REPL
internal/repl          boucle, parsing, registre des commandes, affichage
internal/game          personnage, dés, ennemis/boss, combat, butin, état, save/load
internal/open5e        client HTTP (monsters/, weapons/), structs typées, cache (cache.go)
internal/world         chargement/validation de world.json, navigation
```

Prévu pour être testable sans refactorisation :
- les dés passent par l'interface `game.Roller` (`IntN`), satisfaite par `*rand.Rand` (`math/rand/v2`) ;
- `open5e.NewCache(ttl, interval, open5e.WithClock(now))` accepte une horloge injectable, et `Reap()` est exportée ;
- `open5e.NewClient(baseURL, cache, open5e.WithHTTPClient(...))` permet de viser un `httptest.Server` ;
- `Game.Explore` reçoit un `MonsterFetcher` et `Game.Loot` une liste d'armes, plutôt que le client ;
- le REPL lit un `io.Reader` et écrit dans un `io.Writer`.

## Choix de design

### Règles de base

- **Combat : un round par commande.** `fight` joue un seul round (attaque du joueur, puis riposte de l'ennemi s'il est encore debout). Entre deux rounds, on peut choisir `flee`.
- **Attaques** : d20 + bonus contre la CA. Un 20 naturel est un critique (dés doublés, modificateur fixe non doublé) ; un 1 naturel est un échec automatique. Un coup qui touche inflige au moins 1 dégât. Un ennemi ordinaire choisit **une** action au hasard parmi celles qui ont un `attack_bonus` **et** des `damage_dice` (le `damage_bonus` s'ajoute aux dés) ; sinon, il utilise une attaque par défaut à 1d4.
- **CA du joueur** : le sujet n'en définit pas. Elle vaut 11 (cuir) + mod. DEX, soit 12 au départ.
- **Fuite** : d20 + DEX du joueur contre DD 10 + DEX de l'ennemi. En cas de réussite, le combat s'arrête. En cas d'échec, l'ennemi porte une attaque gratuite, puis le joueur s'enfuit s'il survit.
- **Rencontres** : une région de danger N fait N tirages dans ses `foes` et garde l'ennemi au CR le plus élevé.
- **Niveaux** : le seuil pour quitter le niveau L vaut **L² × 50 XP** (50, 200, 450, 800…), en XP cumulée. Chaque niveau donne +6 PV max (avec soin complet) et +1 au bonus d'attaque. Plusieurs niveaux peuvent s'enchaîner d'un coup.
- **Récompenses** : l'XP suit la table officielle 5e par CR (`internal/game/xp.go`). L'or vaut 1d10 + CR × 10 (× 5 pour un boss).
- **Mort** : réveil à `crossroads` avec 1 PV, combat annulé. Le repos y redevient disponible (nouvelle visite).
- **Repos** : une fois par visite. Le drapeau est remis à zéro à chaque `travel`.

### Bonus : butin et équipement

- Les armes viennent de `/v1/weapons/` (SRD, 37 armes, pagination suivie, réponses en cache). Les armes sans vraie expression de dés (sarbacane `"1"`, filet `"0"`) sont écartées.
- Chaque victoire laisse un **cadavre**, fouillable une seule fois avec `loot`. Il est perdu si tu voyages, si tu repars explorer ou si tu meurs. Il n'est pas sauvegardé.
- **Chance de trouver une arme** : 35 % + 10 % par point de CR (plafond 90 %), et 100 % sur un boss.
- **Qualité** : l'arme est tirée parmi celles dont les dégâts moyens tombent dans la fenêtre [plafond − 3, plafond], avec plafond = 4,5 + 1,5 × CR. Un gobelin lâche du d4 à d8, un CR 3 toujours une arme lourde (2d6 ou 1d12), et un boss uniquement une arme lourde.
- Une arme déjà possédée est revendue (3 or par point de dégâts moyens).
- `equip <weapon>` accepte le slug ou le nom. Il est **interdit en combat** : pas le temps de changer d'arme. Les dégâts utilisent toujours les dés de l'arme équipée.

### Bonus : région de boss

- `world.json` accepte deux champs optionnels par région :
  - `min_level` : niveau requis pour y entrer ;
  - `boss` : `{ "slug", "title", "hit_points" }`.

  La validation vérifie ces champs, et refuse notamment une région de départ scellée.
- **The Hollow Throne** (niveau 7 requis, accessible depuis la Black Spire) abrite **The Hollow King** : un Wight Open5e renommé, ramené à 40 PV.
- **Plusieurs actions** : un boss lit le nombre de sa `Multiattack` (« makes **two** longsword attacks ») et frappe autant de fois par round, avec une attaque tirée au hasard à chaque fois. Ce nombre est borné à [2, 3].
- **Unique** : une fois vaincu, il ne revient jamais (`bosses_defeated` dans la save), et la carte affiche « vaincu ». Si tu fuis ou meurs, il retrouve tous ses PV.

### Équilibrage (mesuré, pas deviné)

Les taux ci-dessous viennent de simulations de 3 000 à 4 000 combats par case, jouées avec le vrai moteur de combat et les vraies fiches SRD.

- **Problèmes constatés** : la Black Spire d'origine (Wraith, Vampire Spawn) donnait 0 % de victoire à tous les niveaux, et la Mummy dépassait à peine 30 %. Les régions ont donc été recomposées :

  | Région | Niveau visé | Victoire au niveau visé |
  |---|---|---|
  | Ashen Marsh / Hollow Wood | 1-2 | 55-97 % |
  | Drowned Chapel | 3-4 | 55-67 % (épée de départ) |
  | Bone Crypts | 4-6 | 55-79 % avec une arme lourde |
  | Black Spire | 5-7 | 68-93 % avec une arme lourde |

- **Boss** : avec seulement 2 attaques par round, les boss SRD bruts étaient imbattables, car le joueur n'a qu'une attaque et une CA de 12. Le Hollow King à 40 PV donne :

  | Arme | Niv. 6 | Niv. 7 | Niv. 8 |
  |---|---|---|---|
  | Épée longue de départ (1d8) | 6 % | 13 % | 23 % |
  | Arme lourde (1d12 / 2d6) | 33-37 % | 46-57 % | 65-75 % |

  Le butin devient donc nécessaire pour finir le jeu.

### Détails techniques

- **Open5e** : la v1 renvoie du HTML (API navigable Django) même avec `Accept: application/json`, car la réponse est en cache chez Cloudflare. Le client ajoute donc `format=json` à chaque URL. Les listes filtrent sur `document__slug=wotc-srd`. Le cache ne stocke que les réponses 200. Un 404 sur un monstre devient `open5e.ErrNotFound` (« monstre inconnu »).
- **Robustesse du REPL** : chaque commande s'exécute sous `recover`. Les erreurs d'écriture sur la sortie sont conservées (sticky error, comme `bufio.Writer`) et vérifiées après chaque commande.

## Exemples de session

Transcriptions réelles, bannière ASCII retirée.

### Partie complète : victoire, butin, mort, repos, sauvegarde, `--continue`

```sh
{ printf "look\ntravel ashen-marsh\nexplore\n"; yes fight | head -3
  printf "loot\ninventory\nequip rapier\ntravel drowned-chapel\nexplore\nsave\n"; yes fight | head -6
  printf "map\nrest\ncharacter\nslain\nsave\nquit\n"; } | ./grimhold --seed 30 --name Morwen
printf "inventory\nquit\n" | ./grimhold --continue
```

```
Les terres sont maudites. Tape help pour connaître tes options.
grimhold > look
== The Crossroads ==
A gibbet creaks over four muddy roads. The last lantern in the land still burns here, for now.
Danger : 0 (un refuge, pour l'instant)
Sorties : ashen-marsh (Ashen Marsh), hollow-wood (Hollow Wood)
Un coin semble assez sûr pour se reposer.
grimhold > travel ashen-marsh
Tu prends la route vers Ashen Marsh...

== Ashen Marsh ==
Fog, iron, and things that breathe under the water.
Danger : 1 (calme inquiétant)
Sorties : crossroads (The Crossroads), drowned-chapel (Drowned Chapel)
Un coin semble assez sûr pour se reposer.
grimhold > explore
Quelque chose remue dans l'ombre... Skeleton (CR 1/4) surgit !
  CA 13 · PV 13 · attaques : Shortsword (+4, 1d6+2), Shortbow (+4, 1d6+2)
Tape fight pour combattre ou flee pour fuir.
grimhold > fight
— Round 1 —
  Tu attaques (Épée longue) : d20 1 +3 = 4 contre CA 13 → 1 naturel, échec critique !
  Skeleton riposte (Shortbow) : d20 5 +4 = 9 contre CA 12 → raté
  PV : toi 20/20 · Skeleton 13/13
Le combat continue : fight ou flee ?
grimhold > fight
— Round 2 —
  Tu attaques (Épée longue) : d20 16 +3 = 19 contre CA 13 → touché, 5 dégâts
  Skeleton riposte (Shortsword) : d20 1 +4 = 5 contre CA 12 → 1 naturel, échec critique !
  PV : toi 20/20 · Skeleton 8/13
Le combat continue : fight ou flee ?
grimhold > fight
— Round 3 —
  Tu attaques (Épée longue) : d20 19 +3 = 22 contre CA 13 → touché, 8 dégâts
  PV : toi 26/26 · Skeleton 0/13
Skeleton s'effondre. Victoire ! +50 XP, +3 or. (Skeleton abattus : 1)
*** NIVEAU 2 ! PV max 26 (soin complet), bonus d'attaque +4 ***
Tape loot pour fouiller le cadavre.
grimhold > loot
Tu fouilles Skeleton et trouves : Rapier (1d8, 4.5 dégâts en moyenne) !
Tape equip rapier pour t'en servir.
grimhold > inventory
Tes armes :
  * Épée longue      1d8    moy.  4.5  (longsword)
    Rapier           1d8    moy.  4.5  (rapier)
(* = équipée ; equip <weapon> pour changer)
grimhold > equip rapier
Tu empoignes Rapier (1d8).
grimhold > travel drowned-chapel
Tu prends la route vers Drowned Chapel...

== Drowned Chapel ==
Black water fills the nave to the knees. The bell still rings at midnight, though no one is left to pull the rope.
Danger : 2 (des yeux vous suivent depuis l'ombre)
Sorties : ashen-marsh (Ashen Marsh), bone-crypts (Bone Crypts)
Un coin semble assez sûr pour se reposer.
grimhold > explore
Quelque chose remue dans l'ombre... Specter (CR 1) surgit !
  CA 12 · PV 22 · attaques : Life Drain (+4, 3d6)
Tape fight pour combattre ou flee pour fuir.
grimhold > save
✗ tu ne peux pas sauvegarder en plein combat (tape fight ou flee)
grimhold > fight
— Round 1 —
  Tu attaques (Rapier) : d20 17 +4 = 21 contre CA 12 → touché, 6 dégâts
  Specter riposte (Life Drain) : d20 12 +4 = 16 contre CA 12 → touché, 7 dégâts
  PV : toi 19/26 · Specter 16/22
Le combat continue : fight ou flee ?
grimhold > fight
— Round 2 —
  Tu attaques (Rapier) : d20 13 +4 = 17 contre CA 12 → touché, 4 dégâts
  Specter riposte (Life Drain) : d20 1 +4 = 5 contre CA 12 → 1 naturel, échec critique !
  PV : toi 19/26 · Specter 12/22
Le combat continue : fight ou flee ?
grimhold > fight
— Round 3 —
  Tu attaques (Rapier) : d20 5 +4 = 9 contre CA 12 → raté
  Specter riposte (Life Drain) : d20 1 +4 = 5 contre CA 12 → 1 naturel, échec critique !
  PV : toi 19/26 · Specter 12/22
Le combat continue : fight ou flee ?
grimhold > fight
— Round 4 —
  Tu attaques (Rapier) : d20 18 +4 = 22 contre CA 12 → touché, 3 dégâts
  Specter riposte (Life Drain) : d20 6 +4 = 10 contre CA 12 → raté
  PV : toi 19/26 · Specter 9/22
Le combat continue : fight ou flee ?
grimhold > fight
— Round 5 —
  Tu attaques (Rapier) : d20 11 +4 = 15 contre CA 12 → touché, 1 dégât
  Specter riposte (Life Drain) : d20 16 +4 = 20 contre CA 12 → touché, 14 dégâts
  PV : toi 5/26 · Specter 8/22
Le combat continue : fight ou flee ?
grimhold > fight
— Round 6 —
  Tu attaques (Rapier) : d20 12 +4 = 16 contre CA 12 → touché, 5 dégâts
  Specter riposte (Life Drain) : d20 20 +4 = 24 contre CA 12 → 20 naturel, CRITIQUE ! 23 dégâts
  PV : toi 1/26 · Specter 3/22
Tout devient noir...
Tu te réveilles à The Crossroads avec 1 PV. Les corbeaux se disputent déjà ta légende. Elle est courte.
grimhold > map
Carte des terres maudites (* = ta position) :
  * crossroads      The Crossroads     danger 0  → ashen-marsh, hollow-wood
    ashen-marsh     Ashen Marsh        danger 1  → crossroads, drowned-chapel
    hollow-wood     Hollow Wood        danger 1  → crossroads, bone-crypts
    drowned-chapel  Drowned Chapel     danger 2  → ashen-marsh, bone-crypts
    bone-crypts     Bone Crypts        danger 3  → hollow-wood, drowned-chapel, black-spire
    black-spire     The Black Spire    danger 4  → bone-crypts, hollow-throne
    hollow-throne   The Hollow Throne  danger 5  → black-spire                               [niveau 7 requis] [boss : The Hollow King]
grimhold > rest
Tu dresses un camp de fortune et dors d'un sommeil agité. +25 PV (26/26).
grimhold > character
Morwen — niveau 2
  PV       : 26/26
  XP       : 50/200 (prochain niveau)
  Attaque  : +4 avec Rapier (1d8)
  Armes    : 2 (inventory pour la liste)
  CA       : 12
  DEX      : +1
  Or       : 3
  Position : The Crossroads (déjà reposé ici)
grimhold > slain
Tableau de chasse :
  skeleton             x1
Total : 1 victime(s).
grimhold > save
Partie sauvegardée dans save.json.
grimhold > quit
La brume se referme derrière toi. À bientôt, si tu survis.

$ ./grimhold --continue
Les terres sont maudites. Tape help pour connaître tes options.
Partie chargée : Morwen, niveau 2, 26/26 PV, à The Crossroads.
grimhold > inventory
Tes armes :
    Épée longue      1d8    moy.  4.5  (longsword)
  * Rapier           1d8    moy.  4.5  (rapier)
(* = équipée ; equip <weapon> pour changer)
grimhold > quit
La brume se referme derrière toi. À bientôt, si tu survis.
```

### Boss : The Hollow King

Pour cette démo, le personnage est chargé depuis une sauvegarde v2 écrite à la main (niveau 7, épée à deux mains, au pied de la Black Spire), afin de ne pas avoir à rejouer toute la montée en niveau :

```sh
printf "load\ntravel hollow-throne\nexplore\nfight\nfight\nfight\nfight\nfight\nfight\nfight\nloot\nmap\nexplore\nquit\n" \
  | ./grimhold --seed 1 --save boss.json
```

```
Les terres sont maudites. Tape help pour connaître tes options.
grimhold > load
Partie chargée : Morwen, niveau 7, 56/56 PV, à The Black Spire.
grimhold > travel hollow-throne
Tu prends la route vers The Hollow Throne...

== The Hollow Throne ==
At the top of the spire, a crown of rust rests on a skull that still remembers being a king. The curse began here.
Danger : 5 (chaque pas ici est une prière)
Sorties : black-spire (The Black Spire)
The Hollow King attend ici. Tape explore si tu l'oses.
Un coin semble assez sûr pour se reposer.
grimhold > explore
Le silence se brise. The Hollow King se lève de son trône (Wight, CR 3) !
  CA 14 · PV 40 · 2 attaques par round : Life Drain (+4, 1d6+2), Longsword (+4, 1d8+2), Longbow (+4, 1d8+2)
Tape fight pour l'affronter ou flee pour fuir.
grimhold > fight
— Round 1 —
  Tu attaques (Greatsword) : d20 11 +9 = 20 contre CA 14 → touché, 6 dégâts
  The Hollow King riposte (Life Drain) : d20 19 +4 = 23 contre CA 12 → touché, 4 dégâts
  The Hollow King riposte (Longsword) : d20 14 +4 = 18 contre CA 12 → touché, 9 dégâts
  PV : toi 43/56 · The Hollow King 34/40
Le combat continue : fight ou flee ?
grimhold > fight
— Round 2 —
  Tu attaques (Greatsword) : d20 11 +9 = 20 contre CA 14 → touché, 8 dégâts
  The Hollow King riposte (Longbow) : d20 4 +4 = 8 contre CA 12 → raté
  The Hollow King riposte (Longbow) : d20 18 +4 = 22 contre CA 12 → touché, 6 dégâts
  PV : toi 37/56 · The Hollow King 26/40
Le combat continue : fight ou flee ?
grimhold > fight
— Round 3 —
  Tu attaques (Greatsword) : d20 12 +9 = 21 contre CA 14 → touché, 9 dégâts
  The Hollow King riposte (Longbow) : d20 17 +4 = 21 contre CA 12 → touché, 7 dégâts
  The Hollow King riposte (Longsword) : d20 20 +4 = 24 contre CA 12 → 20 naturel, CRITIQUE ! 11 dégâts
  PV : toi 19/56 · The Hollow King 17/40
Le combat continue : fight ou flee ?
grimhold > fight
— Round 4 —
  Tu attaques (Greatsword) : d20 6 +9 = 15 contre CA 14 → touché, 6 dégâts
  The Hollow King riposte (Longsword) : d20 3 +4 = 7 contre CA 12 → raté
  The Hollow King riposte (Life Drain) : d20 7 +4 = 11 contre CA 12 → raté
  PV : toi 19/56 · The Hollow King 11/40
Le combat continue : fight ou flee ?
grimhold > fight
— Round 5 —
  Tu attaques (Greatsword) : d20 15 +9 = 24 contre CA 14 → touché, 9 dégâts
  The Hollow King riposte (Life Drain) : d20 7 +4 = 11 contre CA 12 → raté
  The Hollow King riposte (Longsword) : d20 14 +4 = 18 contre CA 12 → touché, 10 dégâts
  PV : toi 9/56 · The Hollow King 2/40
Le combat continue : fight ou flee ?
grimhold > fight
— Round 6 —
  Tu attaques (Greatsword) : d20 2 +9 = 11 contre CA 14 → raté
  The Hollow King riposte (Life Drain) : d20 9 +4 = 13 contre CA 12 → touché, 3 dégâts
  The Hollow King riposte (Life Drain) : d20 4 +4 = 8 contre CA 12 → raté
  PV : toi 6/56 · The Hollow King 2/40
Le combat continue : fight ou flee ?
grimhold > fight
— Round 7 —
  Tu attaques (Greatsword) : d20 6 +9 = 15 contre CA 14 → touché, 10 dégâts
  PV : toi 62/62 · The Hollow King 0/40
The Hollow King s'effondre dans un fracas d'os et de cendres. La malédiction de Grimhold est brisée !
Victoire légendaire ! +700 XP, +185 or.
*** NIVEAU 8 ! PV max 62 (soin complet), bonus d'attaque +10 ***
Tape loot pour fouiller le cadavre.
grimhold > loot
Tu fouilles The Hollow King et trouves : Greataxe (1d12, 6.5 dégâts en moyenne) !
Tape equip greataxe pour t'en servir.
grimhold > map
Carte des terres maudites (* = ta position) :
    crossroads      The Crossroads     danger 0  → ashen-marsh, hollow-wood
    ashen-marsh     Ashen Marsh        danger 1  → crossroads, drowned-chapel
    hollow-wood     Hollow Wood        danger 1  → crossroads, bone-crypts
    drowned-chapel  Drowned Chapel     danger 2  → ashen-marsh, bone-crypts
    bone-crypts     Bone Crypts        danger 3  → hollow-wood, drowned-chapel, black-spire
    black-spire     The Black Spire    danger 4  → bone-crypts, hollow-throne
  * hollow-throne   The Hollow Throne  danger 5  → black-spire                               [niveau 7 requis] [vaincu : The Hollow King]
grimhold > explore
Tu fouilles les environs. Rien ne bouge... The Hollow King n'est plus qu'un tas de cendres ; il n'y a rien à affronter ici.
grimhold > quit
La brume se referme derrière toi. À bientôt, si tu survis.
```
