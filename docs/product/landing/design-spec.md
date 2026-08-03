# SPEC DE DESIGN — Landing page `unseat`
Cible : page HTML unique, laptop-first (1440), responsive tablette (768–1279). Thème clair uniquement. Tous les tokens en variables CSS, zéro couleur en dur.

---

## 0. CONCEPT DIRECTEUR — « CHAÎNE DE GARDE » (Chain of Custody)

La page n'est pas une brochure qui *parle* d'un certificat. **La page est elle-même une pièce versée au dossier.**

Trois conséquences non négociables sur toute la page :

1. **Rail de folio.** Une colonne fixe à gauche (88px) porte la numérotation `§01 → §14`, un filet vertical avec crans, l'horodatage de la section active, et un **sceau** qui reste gris tant que la section n'est pas lue. C'est le squelette visuel reconnaissable en screenshot.
2. **Marge annotée.** Toute affirmation chiffrée ou statutaire de la page est survolable et déclenche une **note de provenance** dans la marge : source, endpoint, horodatage, ou `provider will not say`. La page s'audite elle-même en direct. Traduction UI de « preuve avant promesse ».
3. **La hachure est l'inconnu.** Un motif de hachures 45° à filets fins n'est jamais décoratif : il signifie exactement une chose, partout, sans exception — *l'API s'est arrêtée ici*. Coverage `unknown`, `money_unknown`, `complete_with_provider_limits`, `Provider will not say` : même hachure. C'est le signe distinctif de la marque.

Registre esthétique : **suisse, pas vintage.** Filets 1px, grille stricte, chiffres tabulaires, aucun papier jauni, aucune texture « parchemin ». Le seul emprunt au document administratif est structurel (folios, tampons, colonnes de registre, points de conduite), jamais nostalgique.

Geste final : en bas de page, la page **se tamponne elle-même** (`PAGE LUE — 2026-08-02 22:14 UTC`) et affiche un hash de ce qui a été lu. C'est le moment partageable.

---

## 1. PALETTE

### 1.1 Base

```css
:root {
  /* Papier */
  --paper:          #FAF8F3;  /* fond global */
  --paper-sunk:     #F3EFE6;  /* blocs encastrés, marge, cellules inertes */
  --paper-raised:   #FFFDF8;  /* cartes surélevées, certificat */
  --paper-edge:     #ECE6D9;  /* fond des en-têtes de tableau */

  /* Encre */
  --ink:            #16130F;  /* titres, chiffres clés */
  --ink-2:          #3F3A32;  /* corps de texte */
  --ink-3:          #6B655B;  /* texte secondaire, légendes */
  --ink-4:          #9A9287;  /* labels de champ, méta, numérotation */

  /* Filets */
  --rule:           #D8D1C1;  /* hairline standard 1px */
  --rule-strong:    #B9B09C;  /* séparateur de section, cadre de certificat */
  --rule-hair:      #E6E0D2;  /* grille interne de tableau */

  /* Accent unique — sceau */
  --seal:           #C2402A;  /* tampons, curseur actif, CTA primaire */
  --seal-deep:      #9B3220;  /* hover CTA */
  --seal-wash:      #F8EAE6;  /* fond de tampon, surlignage */
}
```

Interdits confirmés : aucun gradient violet, aucun bleu SaaS (`#3B82F6`, `#6366F1` et voisins bannis), aucun mesh, aucune ombre colorée. **Une seule ombre autorisée dans toute la page** : `0 1px 0 var(--rule)` + `0 8px 24px -16px rgba(22,19,15,.18)` sur le certificat hero et les panneaux flottants. Ailleurs : filets, pas d'ombres.

### 1.2 Statuts de certificat — système d'identité

Cinq statuts, cinq traitements distincts **à la fois par couleur ET par forme** (daltonisme + lisibilité en screenshot N&B).

| Statut | texte | fond | bordure | Signe de forme |
|---|---|---|---|---|
| `complete` | `#14654A` | `#E4EFE9` | `#9CC0B0` | pastille pleine, bord solide |
| `complete_with_provider_limits` | `#14654A` | `#E4EFE9` + **hachure 45°** `#9CC0B0` @ 8px | `#9CC0B0`, **bord droit tireté** | hachure = limites nommées |
| `blocked` | `#9A6408` | `#F6EDDA` | `#D9BE84` | **barre gauche 3px** pleine (attente) |
| `incomplete` | `#A8331C` | `#F8E7E2` | `#E0AC9E` | **contour double** 1px+1px (alerte) |
| `stale` | `#4E5A66` | `#E9ECEF` | `#B3BCC5` | **bord tireté sur 4 côtés** + glyphe `↻` |

```css
--st-complete:     #14654A;  --st-complete-bg:   #E4EFE9;  --st-complete-br:   #9CC0B0;
--st-limits:       #14654A;  --st-limits-bg:     #E4EFE9;  --st-limits-br:     #9CC0B0;
--st-blocked:      #9A6408;  --st-blocked-bg:    #F6EDDA;  --st-blocked-br:    #D9BE84;
--st-incomplete:   #A8331C;  --st-incomplete-bg: #F8E7E2;  --st-incomplete-br: #E0AC9E;
--st-stale:        #4E5A66;  --st-stale-bg:      #E9ECEF;  --st-stale-br:      #B3BCC5;
```

Hiérarchie de gravité assumée : `incomplete` (accès ouvert sans décision) est **plus rouge** que `blocked` (en attente d'une personne). Choix produit, pas hasard chromatique — ne pas les intervertir.

### 1.3 Claims billing

Même famille, dérivée des statuts — pas un second système.

| Claim | Traitement |
|---|---|
| `saving_verified` | pastille pleine `--st-complete` + glyphe `✓` |
| `seat_reclaim_verified` | **contour seul** `--st-complete`, fond `--paper` (vérifié, mais pas de l'argent) |
| `renewal_opportunity` | `--st-blocked` (ocre), glyphe `→` (l'économie arrive plus tard) |
| `money_unknown` | **aucune couleur** : `--ink-3` sur hachure `--rule-strong`. Interdit de le colorer. |
| `procurement_required` | `--ink` sur `--paper-sunk`, **contour double** + micro-tampon `HUMAN` |

```css
--bl-verified:    var(--st-complete);
--bl-reclaim:     var(--st-complete);   /* outline-only */
--bl-renewal:     var(--st-blocked);
--bl-unknown:     var(--ink-3);          /* + var(--hatch) */
--bl-procurement: var(--ink);
```

### 1.4 Hachure (token global)

```css
--hatch: repeating-linear-gradient(45deg,
          transparent 0 3px,
          color-mix(in srgb, currentColor 22%, transparent) 3px 4px);
```
Un seul token, réutilisé par héritage de `currentColor`. Angle 45°, pas de 4px. **Jamais** en décor de fond ou sur une section entière.

---

## 2. TYPOGRAPHIE

Trois familles Google Fonts. `preconnect` + un seul `<link>`, `display=swap`, sous-ensemble `latin`.

| Rôle | Famille | Graisses | Usage |
|---|---|---|---|
| Display | **Fraunces** (variable, `opsz` 72, `SOFT` 0, `WONK` 0) | 400, 600 | H1, H2, chiffres héros |
| Texte | **Archivo** | 400, 500, 600 | corps, UI, labels |
| Mono | **IBM Plex Mono** | 400, 500, 600 | certificat, evidence, terminal, YAML, tous les identifiants et statuts |

Fraunces avec `WONK 0` reste sobre ; ne pas activer l'axe wonk, il casserait la neutralité suisse. Archivo apporte la rigueur grotesque sans être Inter. Plex Mono est le seul admis pour le certificat.

### 2.1 Échelle (laptop 1440 ; root 16px)

| Token | Taille / interligne | Famille & graisse | Emploi |
|---|---|---|---|
| `--t-hero` | 60/62, `-0.022em` | Fraunces 600 | H1 unique |
| `--t-h2` | 38/44, `-0.016em` | Fraunces 600 | H2 de section |
| `--t-h3` | 22/30, `-0.008em` | Archivo 600 | sous-titres, titres de carte |
| `--t-lead` | 20/32 | Archivo 400 | chapeau sous H1/H2 |
| `--t-body` | 17/28 | Archivo 400 | corps |
| `--t-small` | 14/22 | Archivo 400 | légendes, notes de marge |
| `--t-label` | 11/14, `0.12em`, UPPERCASE | Archivo 600 | labels, en-têtes de colonne, `§` |
| `--t-mono` | 15/26 | Plex Mono 400 | certificat, terminal, YAML |
| `--t-mono-sm` | 13/22 | Plex Mono 400 | cellules de matrice, evidence |
| `--t-stat` | 44/44, `-0.02em`, `tnum` | Plex Mono 500 | grands compteurs |

Global : `font-variant-numeric: tabular-nums` sur **tout** chiffre. `text-wrap: balance` sur H1/H2, `pretty` sur les chapeaux. Ligne de corps 66–72 caractères max.
Tablette : `--t-hero` 42/46, `--t-h2` 30/36, `--t-stat` 34, reste inchangé.

---

## 3. GRILLE & MOTIFS

### 3.1 Grille
- Page `--page-max: 1320px`. Rail de folio 88px fixe à gauche (≥1200px), contenu 12 colonnes, gouttière 24px, largeur utile 1000px, **marge d'annotation 200px à droite** (notes de provenance).
- <1200px : rail replié en bandeau horizontal sticky de 36px (numéro + titre + sceau) ; marge d'annotation → tiroir sous l'élément.
- Espacements : 4/8/12/16/24/32/48/64/96. Section : 96px haut/bas, séparées par un **filet de section** pleine largeur `--rule-strong` interrompu par le numéro `§07`.

### 3.2 Répertoire de motifs
1. **Filet de section.** 1px `--rule-strong`, interrompu à 88px par le folio en `--t-label` sur fond `--paper`. Titre aligné sur le filet, jamais centré.
2. **Coins de repérage.** Cartes majeures (certificat, panneau recheck, matrice) : quatre **traits de coupe** 8px `--rule-strong`, décalés de 6px hors cadre. `border-radius` ≤ 2px sur les objets « document » ; 4px sur l'UI vivante (boutons, chips, onglets). Contraste voulu : le document est dur, l'interface est douce.
3. **Points de conduite.** Toute paire label→valeur reliée par des points `--rule` : `Access closed ······················· 12`. Signature immédiate.
4. **Hachure** — §1.4. Uniquement `unknown`.
5. **Tampon.** Cercle double filet 1px + 2px, `--seal`, rotation `-7deg`, Plex Mono 600 `--t-label`, opacité .9, `mix-blend-mode: multiply`. **Trois occurrences max** : hero (`VERIFIED 2026-08-02`), fin §10 (`EVIDENCE ON FILE`), fin de page (`PAGE LUE`).
6. **Barres de rédaction.** Rect `--ink` plein sur le texte. Survol : jamais le secret, uniquement le **résumé de rédaction** (`redacted: bearer token, 41 chars, sha256:9f2c…`).
7. **Perforation.** Pointillé vertical 3px séparant les deux moitiés du certificat hero (identité | comptages), comme un talon détachable.
8. **Grain.** `feTurbulence baseFrequency=0.9 numOctaves=3` en background fixe, opacité **2,5 %**, `multiply`, uniquement sur `--paper`. Imperceptible de près, visible en screenshot. Désactivé <768px.
9. **Numérotation de registre.** Index `01`, `02`… en `--ink-4` `--t-label`, colonne 28px. Tableaux à **filets horizontaux uniquement**, sans bordure extérieure ni zébrures.
10. **Curseur de lecture.** Le rail affiche un horodatage UTC live (`22:14:07 UTC`) réellement incrémenté. Détail mémorable, cohérent avec « les certificats périment ».

---

## 4. LAYOUT PAR SECTION

**Compo** = composition · **Comp** = composant interactif.

### §01 — Hero
**Compo.** Rupture d'axe : colonne gauche 5/12 (H1 + chapeau + CTA), colonne droite 7/12 = certificat, **décalé −32px vers le haut** et **débordant de 40px hors de la marge droite**, jusqu'à la marge d'annotation. Le certificat chevauche le filet §01/§02 de 24px — il « dépasse du dossier ». H1 sur 3 lignes, ferré à gauche ; aucune centralisation dans toute la page.

**Comp — Certificat interactif.**
- Cadre `--paper-raised`, filet `--rule-strong`, traits de coupe, en-tête `OFFBOARDING CERTIFICATE` en `--t-label` + filet double.
- **Sélecteur de sujet** : trois onglets discrets `Alice Martin` / `Marco Reis` / `svc-deploy-bot`, rendant trois statuts différents (`complete_with_provider_limits`, `blocked`, `stale`). Le changement **retypographie** le certificat ligne par ligne (30ms de décalage). Trois cas, pas un cas favorable.
- Chaque ligne survolable → **note de provenance** en marge (`source: github /orgs/{org}/installations · collected 22:14:03 UTC · installer: audit log, Enterprise only`). Les lignes `Provider will not say` disent *pourquoi*.
- `Status` = chip cliquable → ancre §04, la position correspondante de l'échelle s'allume.
- Tampon `VERIFIED / 2026-08-02` posé en fin d'animation de chargement.
- Zéro montant en euros dans le hero. `Paid seats released 4` est la seule métrique monétisable (note du draft respectée).

**CTA.** Primaire : bloc plein `--seal`, texte `--paper`, 4px, `--t-body` 500 ; sous-ligne `--t-small` `--ink-3`. Secondaire : lien souligné filet 1px `--rule-strong`, offset 4px, épaissi au survol — pas de second bouton.

### §02 — Problème
**Compo.** Texte en 6/12 à gauche. À droite, **diagramme de porte** minimal : rectangle « IdP » plein filet + six ouvertures hachurées étiquetées (`accounts outside IdP`, `external identities`, `GitHub Apps`, `billed seats`, `ownership`, `provider silence`). Aucune icône, aucun cadenas.
**Comp.** La liste du draft devient un **registre numéroté** `01`–`06`, points de conduite vers une colonne droite disant ce que l'IdP répond : `not federated`, `no signal`, `out of scope`… Survol d'une ligne → l'ouverture correspondante s'allume en `--seal`.

### §03 — Promesse produit
**Compo.** **Deux colonnes physiquement séparées par un filet vertical pleine hauteur** : `FACTS` (gauche) et `GUESSES` (droite). La colonne droite est **vide**, hachurée, une seule ligne : `intentionally empty`. Geste le plus fort de la section — la séparation fait/supposition est montrée, pas décrite.
**Comp.** Six lignes dans FACTS, chacune avec sa provenance en marge, chacune avec un lien inline `→ §10 evidence`.

### §04 — Le certificat (échelle + recheck)
**Compo.** Deux blocs empilés désalignés : échelle en 10/12 ferrée à gauche, panneau recheck en 7/12 ferré à **droite**, décalé +48px, chevauchant l'échelle de 16px.

**Comp A — Échelle des 5 statuts.** Rail horizontal, 5 crans, poignée draggable (+ clavier `←/→`, `role="slider"`). Chaque position : change la définition affichée, **repeint un mini-certificat lié de 240px** à droite de l'échelle, applique couleur **et** forme (§1.2). Ce n'est pas une progression : **aucun remplissage à gauche**, seulement des crans, et la note `this is not a progress bar` sous l'échelle. « Aucun d'eux n'est un spinner. »

**Comp B — Panneau recheck J+7.** Bouton `Run recheck (J+7)` → les trois lignes du draft s'écrivent en mono à 220ms d'intervalle, `ok` en `--st-complete`. La troisième (`Webhook wh_3391 re-enabled`) déclenche : mini-certificat → `stale` (bord tireté + `↻`), **et le sceau du rail de folio passe en gris avec un `↻`**. Bouton `Reset` discret. Interaction la plus démonstrative de la page : la placer au-dessus du pli de la section.

### §05 — Résolution d'identité
**Compo.** Tableau de registre 10/12, en-têtes `--paper-edge`, index numérotés, colonne `Strength` à droite.
**Comp — Curseur de seuil (idée non demandée #1).** Curseur `Match strength required to act`, de `weak` à `strong`. Le glisser vers `strong` fait **basculer des lignes** de `will act` vers `goes to review` (FLIP, 240ms). Clé : **butée dure** — impossible de descendre sous `strong` ; en poussant contre la butée, un tampon `POLICY` apparaît et affiche `nothing is removed on a weak match alone`. Une UI qui refuse d'obéir vaut dix paragraphes.
Sous le tableau : `matched` / `unmatched` / `ambiguous`, ce dernier en `--st-blocked` avec `question for a human`.

### §06 — Descendants numériques
**Compo.** Diagramme SVG horizontal 4 colonnes, hors grille (déborde de 48px des deux côtés — seule section à le faire) : `person → objects → decisions → evidence`. Connecteurs = filets 1px `--rule-strong`, orthogonaux, angles droits, **jamais de courbes de Bézier**. Nœuds = rectangles mono `--paper-raised`.
**Comp — Le bouton qui refuse (idée non demandée #2).** Cliquer `token tk_88f (created by Alice, 2 years ago)` ouvre une fiche latérale à trois actions. `Revoke` est **présent mais désactivé**, raison en clair : `unseat never revokes a credential because its creator left. Ownership is unclaimed — that is a different problem.` Actions actives : `Ask who owns it`, `Open decision`. Survol du bouton désactivé → dépendances connues (`Finance nightly job — last run 3h ago`). Démonstration littérale de creator ≠ owner ≠ consumer.
Le chemin `person → token → decision → evidence` s'illumine en `--seal` ; les branches où l'API s'arrête finissent en **segment hachuré** + `stops here`.

### §07 — Comment ça marche
**Compo.** Bandeau de mode sticky (56px, `--paper-raised`, filet bas) en haut de section, contenu en 9/12 ferré à gauche, **matrice d'écriture permanente** dans la marge droite.
**Comp — Sélecteur Observe / Approve / Enforce.** Trois crans matérialisés (trois cases carrées jointives séparées par un filet, cran actif `--ink` fond / `--paper` texte — pas des onglets arrondis). Sous le sélecteur, **matrice de permissions** persistante : lignes `read` / `propose` / `suspend` / `remove` / `revoke credential` × 3 modes.
- Observe × écriture : hachurée + verrou, `cursor: not-allowed` ; au clic → `Observe cannot be made writable.`
- `revoke credential` × Enforce : `never`, `--st-incomplete` contour double, sur **toutes** les colonnes.

Contenu par mode :
- **Observe** — terminal `unseat offboard alice@company.com`. Cadre mono, en-tête `zsh — unseat` (pas de trois pastilles), fond `--paper-sunk` — **pas de terminal noir**, thème clair strict. Bouton `Run`, sortie ligne par ligne ~18ms, curseur bloc `--seal`. Les lignes `unknown` sortent hachurées. `Copy` en haut à droite. Ligne de synthèse finale + lien `→ voir le certificat`.
- **Approve** — carte de décision : action proposée, raison, approbateur, version de politique + **frise de cycle de vie** `proposed → approved → executed → verified` en 4 crans (franchis en `--st-complete`, suivants en filet). Sous la carte : `this action class approved 14× without modification` avec 14 micro-tampons alignés — l'argument d'automatisation rendu visible.
- **Enforce** — bloc YAML colorisé (`--ink` / `--ink-3` / `--seal` sur les valeurs sensibles ; `never` en `--st-incomplete`). Dessous, l'encart d'audit `Executed in Enforce because…` en `--paper-sunk` avec tampon `RULE R`.

Encart invariant pleine largeur, `--paper-sunk`, filet gauche 3px `--ink` : « Removal is driven by the directory, not by your mappings. » Traiter en **note marginale d'ingénieur**, pas en bandeau marketing.

### §08 — Coverage
**Compo.** Matrice objet × verbe 11/12, en-tête collant, filets horizontaux uniquement.
**Comp — Matrice avec unknowns assumés.**
- Chips de provider au-dessus (`github`, `linear`, `figma`, `slack`…), défaut `github` — le plus riche en unknowns, on montre le pire cas.
- Cellules : `read` / `remove` / `transfer` / `release` (pastilles `--st-complete` outline), `partial` (`--st-blocked`, demi-remplissage), `unknown` (**hachure, jamais case vide**).
- **Compteur d'honnêteté** en haut à droite, `--t-stat` : `17 / 96 unknown`, légende `displayed, not hidden`. **Aucun pourcentage de couverture agrégé** — il inviterait à la comparaison marketing.
- Survol de cellule → note de marge : endpoint réel ou raison du silence.
- Sous la matrice : `generated from source at build time · commit ca35517`. La garantie anti-dérive du draft, rendue littérale.
- **Interdit** : logos de providers. Noms en mono, minuscules, comme dans la config. C'est *la* décision qui distingue cette page de toutes les autres du secteur.

### §09 — Billing
**Comp A — Comparateur de sièges.** Deux barres horizontales, même origine : `billed 48` (filet plein) et `filled 41` (plein `--ink`). Delta de 7 **hachuré**, étiqueté `prepaid block or plan minimum — a finding on its own`. Aucune devise. Graduation en sièges, ticks tous les 10.
**Comp B — 5 claims.** Registre 3 colonnes : claim (mono) / condition / traitement visuel. `money_unknown` en gris hachuré, volontairement le moins attirant. Pied de section, encart contour double : `There is no line for "estimated savings".` — Fraunces 400 italique, seule italique display de la page.
**Idée non demandée #3 — le champ qui refuse la saisie.** Faux champ `cost_per_seat`, placeholder `enter your price…`. Au focus : le champ se verrouille, se hachure, affiche `unseat never asks you to keep prices in YAML.` Il consomme le geste de l'utilisateur pour délivrer le principe. `readonly` + `aria-describedby`, non bloquant, ne gêne jamais le scroll.

### §10 — Evidence
**Compo.** **Deux rendus du même enregistrement**, côte à côte 6/6, bascule central `JSON + hashes` ⇄ `Readable certificate` (vrai commutateur à deux positions, pas des onglets).
**Comp.** JSON en Plex Mono 13px, numéros de ligne en gouttière `--ink-4`. Valeurs sensibles = **barres de rédaction** ; survol → résumé de rédaction, jamais le secret. Hashes tronqués + `copy` au survol. Le rendu « certificat lisible » réutilise **exactement le cadre du hero** — la boucle se ferme. Tampon `EVIDENCE ON FILE`. Clôture en `--t-small` : « Connectors get copied. Rules get copied. A retained record does not. »

### §11 — Pour qui
**Compo.** Trois colonnes 4/4/4 : `Head of IT / IT Ops` (accentuée : `--paper-raised` + filet, décalée −16px vers le haut), `Security`, `Compliance`, puis `Finance` en **quatrième colonne décalée vers le bas de 32px** avec la mention `shows up second`. La composition dit la hiérarchie d'achat.
**Comp.** Encart bas de section pour BetterCloud / Torii / Zluri / Okta : texte seul, **noms en mono, aucun logo**, cadre `--paper-sunk`, filet gauche `--seal`. Positionnement, pas attaque.

### §12 — Pricing
**Compo.** Tableau de registre, **pas de cartes**. 4 lignes, index `01`–`04`, `Size` aligné à droite en tabulaires, colonne `Evidence retention` mise en avant (`--paper-edge`) — c'est l'axe de prix inhabituel, il doit se voir.
**Comp.** La colonne `Mode` réutilise **les chips exacts** du §07 → continuité. Aucun prix affiché (le draft n'en donne pas) : colonne finale = `Talk to us` par ligne. Sous le tableau, trois « never » en `--t-small` `--ink-3`, puces carrées : jamais par connecteur, jamais un % des économies, jamais par identité non humaine.

### §13 — Pourquoi unseat
**Compo.** Bloc pleine largeur, texte en 8/12 (colonne centrée, typographie ferrée à gauche).
**Idée non demandée #4 — « Vendor mode », la plus audacieuse.** Interrupteur `Show what a vendor dashboard would claim`. Activé : un calque se superpose — coches vertes partout, courbe d'« économies estimées », score de risque `A+`, mur de logos. Après 1,2s, un **trait de rature diagonal** `--seal` traverse le calque, chaque élément faux se replie, le texte réel réapparaît avec : `None of those numbers came from an API.` Le calque doit être crédible pour que la démolition porte, mais porter un badge `simulated` permanent pour ne jamais être screenshoté hors contexte. **Premier composant à couper** si le périmètre se réduit : puissant mais non essentiel.

### §14 — CTA final
**Compo.** Section centrée, 480px, fond `--paper-sunk` + grain, filets doubles haut et bas. H2 `--t-h2`, trois instructions numérotées `01 connect your directory / 02 connect one provider / 03 read the certificate, unknowns and all`.
**Comp — Le tampon de page.** À l'entrée en viewport : le sceau du rail se remplit, puis un tampon `PAGE LUE — 2026-08-02 22:14 UTC` se pose en rotation `-7deg`, avec `sections read 14/14 · unknowns shown 17 · sha256:9f2c41…`. Le hash porte sur les sections réellement vues. Moment de partage social, et honnête.
**Footer.** Une seule ligne mono, filets, aucune colonne de liens gonflée.

---

## 5. MOTION

Philosophie : **la page se compose comme un document se dactylographie.** Rien ne rebondit, rien ne glisse de loin. Courbe unique `--ease: cubic-bezier(0.2, 0.7, 0.2, 1)`. Durées `--dur: 220ms`, `--dur-l: 420ms`.

**Page load — 1,45s, non bloquante.**
| t | événement |
|---|---|
| 0 | fond + grain présents (aucun fade global de page) |
| 0–260ms | rail de folio : filet vertical **dessiné** de haut en bas (`scaleY`, origine top) |
| 120ms | H1 : 3 lignes, `translateY(10px)` + fade, décalage 60ms |
| 320ms | chapeau + CTA, décalage 50ms |
| 400ms | cadre du certificat : 4 filets dessinés (2×`scaleX`, 2×`scaleY`), 300ms |
| 640ms | champs du certificat : fade + `translateY(6px)`, décalage **28ms**, points de conduite remplis de gauche à droite (`background-size` animé) |
| 1180ms | tampon `VERIFIED` : `scale(1.06)→1`, `opacity 0→.9`, 180ms, **aucun rebond** |
| 1400ms | l'horodatage du rail démarre |

**Scroll reveals.** `IntersectionObserver` seuil 0.15, `translateY(8px)` + opacité, `--dur-l`, décalage 40ms entre enfants directs, **6 enfants animés max** par section (au-delà, tout ensemble). Jamais d'animation ligne à ligne au-delà de 8 lignes de tableau. Le rail met à jour `§NN` en **cut, pas en fade** (compteur mécanique).

**Micro-interactions (120ms).**
- Ligne de certificat / cellule de matrice : fond `--paper-sunk` + filet gauche 2px `--seal` en `box-shadow: inset` (jamais `border` → zéro reflow). Note de marge en fade 120ms, sans mouvement.
- Boutons : `--seal` → `--seal-deep`, `translateY(-1px)`, `box-shadow: 0 3px 0 var(--seal-deep)` ; `:active` → `translateY(1px)`, ombre 0.
- Chips de statut : la hachure se **décale de 4px** en boucle 800ms au survol uniquement (`background-position`) — seul mouvement continu autorisé.
- Liens secondaires : filet 1px → 2px.
- Focus visible partout : `outline: 2px solid var(--seal); outline-offset: 3px`.

**Terminal.** Écriture **par ligne** (pas par caractère — plus lisible, moins coûteux), 18ms/ligne, curseur bloc `--seal` clignotant 1s `step-end`. Hauteur du conteneur réservée dès le départ : **zéro CLS**.

**`prefers-reduced-motion: reduce`** : translations et dessins de filets supprimés, opacités instantanées, terminal et recheck rendus en état final avec un `Replay` sans animation. Tampon sans scale.

---

## 6. ANTI-LISTE

**Du draft (non négociable)** : dashboard SaaS générique · blobs en gradient · graphe d'économies avec des chiffres qu'aucune API n'a produits (sauf le calque `simulated` de §13, raturé) · mur de logos comme récit de coverage · peur sécuritaire sans le workflow qui y répond.

**Extensions :**
- Aucun **thème sombre**, aucun toggle de thème, aucun terminal à fond noir.
- Aucun **violet, indigo, bleu SaaS**, aucun gradient, aucune ombre colorée, aucun glassmorphism/neumorphism.
- Aucun **logo de provider**, favicon de vendor, avatar client, testimonial, « trusted by ».
- Aucun **badge de conformité** (SOC 2, ISO) non obtenu — la page est un argument d'honnêteté, un badge décoratif la détruit.
- Aucune **coche verte** hors des statuts §1.2. Pas de `✓` décoratif dans les listes.
- Aucun **pourcentage de couverture agrégé**, score de risque, jauge, donut, compteur animé vers un chiffre inventé.
- Aucun **spinner** nulle part, y compris en état de chargement — le draft en fait un point produit. États nommés ou skeletons en filets.
- Aucune **icône illustrative** (cadenas, bouclier, nuage, fusée). Glyphes admis : `→ ↻ ✓ §` et les traits du diagramme.
- Aucune **case vide** dans une matrice. Vide = mensonge ; hachure = `unknown`.
- Aucun **montant en devise** hors `saving_verified`, et aucun dans le hero.
- Aucun **chat widget**, cookie banner intrusif, popup exit-intent, modal newsletter, sticky bar promo.
- Aucun **carrousel**, aucun accordéon masquant du contenu de fond (les inconnues ne se replient pas), aucun `overflow: hidden` qui cacherait un unknown.
- Aucun **parallax**, animation continue pilotée par le scroll, `scroll-jacking`, ni élément en mouvement permanent hors horodatage et hachure au survol.
- Aucun **emoji**, aucune exclamation dans la copy d'interface.
- Aucun **arrondi > 4px** sur les objets « document » ; `border-radius: 9999px` réservé au sceau (cercle).
- Aucune **quatrième famille typographique**, aucune icon font.

---

## 7. NOTES D'IMPLÉMENTATION

- **Thème** : tous les tokens dans `:root`. Chaque statut = une classe `.st--complete` … `.st--stale` qui pose `color`/`background`/`border` **et** son signe de forme. Un composant ne choisit jamais une couleur, il choisit un statut.
- **Accessibilité** : contrastes vérifiés, tous les textes de statut ≥ 4.5:1 sur leur fond. Statut jamais porté par la seule couleur (§1.2). Échelle et curseur de seuil en `role="slider"` + `aria-valuetext` explicite. Notes de marge en `aria-live="polite"` (jamais assertive). Cibles tactiles ≥ 44px sur tablette.
- **Responsive tablette** : rail → bandeau sticky ; certificat sous le H1, pleine largeur, sans débordement ; matrice coverage en scroll horizontal, première colonne collante + ombre de bord `--rule-strong` ; diagramme §06 en vertical 4 niveaux ; §10 en bascule séquentielle (un rendu à la fois) ; marge d'annotation → tiroir déplié au `tap`.
- **Performance** : 3 fonts, `latin` seul, `font-display: swap`, `size-adjust` sur les fallbacks (anti-CLS). Grain en SVG data-URI (<1 Ko). Aucune librairie d'animation — CSS + `IntersectionObserver`. Hauteurs réservées sur tous les conteneurs animés.
- **Ordre de construction si le périmètre se réduit** : (1) certificat hero + rail, (2) échelle + recheck J+7, (3) matrice coverage, (4) sélecteur de modes + terminal, (5) diagramme descendants, (6) evidence, (7) curseur de seuil §05, (8) vendor mode §13. Les items 1–3 suffisent à ce que la page ait son identité.
