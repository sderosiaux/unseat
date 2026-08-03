# Spec Produit: unseat comme certificat d'offboarding SaaS

Date: 2026-08-02

Statut: version canonique de travail, avant implementation SaaS

## These

unseat ne doit pas devenir un dashboard SaaS. Le produit doit prouver qu'un
depart est termine.

Promesse de travail:

> Quand quelqu'un part, unseat montre quels acces humains et non-humains sont
> fermes, lesquels restent ouverts, lesquels ne peuvent pas etre connus, et la
> preuve de tout cela.

Le wedge n'est pas "offboarding automation". Cette promesse existe deja chez
BetterCloud, Torii, Zluri et les workflows IdP. Le wedge defendable est le
**certificat de completude**:

- ce qui a ete trouve;
- ce qui a ete ferme;
- ce qui exige un transfert de responsabilite;
- ce qui exige une approbation;
- ce que le provider ne permet pas de faire;
- ce que le provider refuse de dire;
- la preuve horodatee de chaque decision et action.

Le client n'achete pas un graphe, un rapport, ou une liste de connecteurs. Il
achete la confiance qu'un depart ne laisse pas de comptes, apps, tokens, webhooks
ou agents orphelins derriere lui.

## ICP et acheteur de depart

Fenetre cible probable: **150 a 1000 employes**.

Pourquoi cette fenetre:

- assez de SaaS pour que l'offboarding soit incomplet;
- assez d'audit/security pressure pour que la preuve ait de la valeur;
- pas toujours assez de budget ou de maturite pour BetterCloud/Torii/Zluri;
- un Head of IT ou IT Ops peut essayer un CLI avant d'acheter une plateforme;
- l'organisation a souvent un IdP, mais pas une gouvernance SaaS complete.

Acheteur initial probable: **Head of IT / IT Ops**.

Influenceurs:

- Security: pousse sur le risque residuel et les non-human identities;
- Compliance: pousse sur les preuves;
- Finance/procurement: pousse sur les seats et renouvellements, mais n'est pas
  le premier acheteur du wedge.

Ce qui ferait changer ce cadrage:

- trois entretiens consecutifs ou le CFO/procurement possede deja le probleme;
- trois entretiens consecutifs ou les equipes IT disent que l'offboarding est
  resolu, mais que les NHI/service accounts sont leur douleur principale;
- un canal de distribution qui marche mieux en SaaS hosted qu'en CLI.

## Realite du code actuel

Chiffres du repo au 2026-08-02:

| Capacite | Etat |
|---|---:|
| Connecteurs provider reels | 54 |
| `CanRemove: true` | 50 |
| `CanSuspend: true` | 9 |
| `ReportsActivity: true` | 10 |
| `BillingProvider` | 3 (`github`, `github-copilot`, `linear`) |
| `CredentialProvider` | 2 (`github`, `linear`) |
| Revocation de credential | 0 |

Ce tableau est le garde-fou de la roadmap. unseat sait deja **faire** beaucoup
de suppressions, mais sait **prouver** beaucoup moins de choses:

- peu de providers exposent une vraie activite utilisateur;
- tres peu exposent billing;
- tres peu exposent les non-human identities;
- aucun ne sait encore revoquer un credential;
- l'attribution des GitHub Apps depend de l'audit log Enterprise et reste
  partielle.

La profondeur utile doit donc se mesurer par objet et par verbe, pas par logo:

```text
provider: github
objects:
  users: read, remove
  org_billing: read
  copilot_seats: read
  apps: read
  deploy_keys: unknown
  actions_secrets: unknown
  runners: unknown
  ownership_transfer: unknown
```

Un connecteur profond vaut plus que dix connecteurs qui ne font que `ListUsers`.

## Principes Produit

### 1. Action before dashboard

Un dashboard est utile seulement s'il produit une decision. Le produit doit donc
modeler la decision et l'action avant la visualisation: jugement, risque,
preconditions, approbation, execution, preuve.

### 2. Semi-automatic before fully automatic

L'automatique se gagne. Une classe d'action ne passe en Enforce qu'apres des
decisions repetitives, stables, documentees, sur un provider verifie.

### 3. Human and non-human identities are peers

Un compte humain n'est qu'une identite parmi d'autres. OAuth apps, API keys,
service accounts, webhooks, bots et agents AI ont aussi un owner, un scope, une
surface de risque et un cycle de vie.

### 4. Creator is not owner, owner is not consumer

Une cle creee par Alice peut alimenter un job critique appartenant a l'equipe
Finance. Son depart ne rend pas la cle supprimable. Il rend son ownership
suspect.

Le produit doit separer:

| Notion | Question | Source probable |
|---|---|---|
| Creator | Qui a cree ou autorise l'acces ? | API provider, parfois |
| Owner | Qui en repond aujourd'hui ? | attestation humaine |
| Consumer | Qu'est-ce qui en depend ? | usage vendor, secret store, CI/CD, rarement complet |

Invariant produit:

> La suppression d'un seat est pilotee par le directory, pas par les mappings.
> La revocation d'un credential est pilotee par un non-usage verifie, pas par le
> depart de son createur.

### 5. Spend is prioritization, not the product

Le billing aide a trier les actions. Il ne doit pas forcer unseat a inventer un
prix. Billing reste API-only: si le provider ne dit pas le montant, le produit
peut dire "billed seats", "filled seats", "unknown money", mais pas "saving X".

### 6. Evidence by default

Chaque decision et chaque action doivent produire un dossier de preuve:

- etat avant;
- regle appliquee;
- mode Observe/Approve/Enforce;
- approbateur ou justification d'automatisation;
- appel execute;
- resultat provider;
- verification apres;
- limites et inconnues explicites.

L'evidence n'est pas un log technique. C'est le produit qui s'accumule avec le
temps.

## Modes Produit

### Observe

Lecture seule. Aucun changement provider.

Objectif: montrer ce qui est vrai, ce qui est inconnu, et ce que le produit
pourrait proposer si le client passait en Approve.

Equivalent existant: `scan`, `audit`, une partie de `sync plan`.

### Approve

Le produit genere des decisions et actions proposees, mais attend une validation
humaine.

Objectif: apprendre quelles actions sont stables, collecter la preuve de
decision, et construire la confiance vers Enforce.

### Enforce

Execution automatique, mais jamais globale.

Enforce est perimetre par:

- classe d'action;
- provider;
- capability verifiee;
- niveau de preuve;
- grace period;
- historique d'approbations precedentes.

Exemple:

```yaml
enforce:
  github:
    suspend_user:
      when: directory_user_suspended
      after: 72h
      require_provider_verified: true
    revoke_credential: never
```

Une sortie d'audit doit pouvoir dire:

```text
Executed in Enforce because this exact class of action was approved 14 times
without modification, on verified provider github, under rule R.
```

## Modele Coeur

### Evenement racine: depart d'un humain

Le depart est l'evenement que tout le monde comprend:

```text
alice@company.com left the company
```

L'erreur serait de supposer que quelqu'un tapera toujours cette commande. Le
declencheur vit souvent ailleurs:

- suspension Google Workspace;
- termination date HRIS;
- ticket IT;
- webhook du SIRH;
- CSV de depart;
- Slack/Jira workflow existant.

Le produit doit accepter `offboard <email>` manuellement au debut, mais la
direction SaaS doit traiter HRIS comme une source d'identite, pas comme un SaaS
target.

Invariant de securite:

> Un statut HRIS inconnu ne doit jamais etre interprete comme un depart. Il doit
> rester actif ou aller en review jusqu'a preuve explicite de termination.

### Resolution d'identite

La resolution d'identite est un produit, pas un detail de config. Le certificat
ne vaut rien si `alice@company.com`, `alice@gmail.com`, `alice-dev`,
`a-smith`, et une GitHub App installee par Alice sont lies sans preuve.

Modele minimal:

```text
CanonicalIdentity
  primary_email
  directory_status
  employment_source
  aliases[]
  confidence
  evidence[]
```

Types d'association:

| Type | Exemple | Confiance |
|---|---|---|
| directory_primary | Google Workspace `primaryEmail` | forte |
| directory_alias | Google Workspace alias | forte |
| explicit_mapping | alias declare en config | forte mais manuelle |
| provider_verified_email | email verifie expose par le provider | forte |
| provider_username_match | `alice` ressemble a l'email | faible |
| personal_email_match | adresse perso mappee a un humain | forte si declaree |

Regles P0:

- aucune suppression basee seulement sur un match faible;
- tout alias manuel apparait dans l'evidence;
- toute identite non resolue va en review, pas en removal;
- les mappings disent quel provider existe, pas combien coute un seat;
- la resolution doit produire `matched`, `unmatched`, ou `ambiguous`.

### Descendance numerique

Remplacer "identity graph" par une requete bornee:

```text
digital_descendants(alice@company.com)
```

Elle cherche:

- comptes SaaS;
- roles et groupes;
- seats payants;
- OAuth apps installees;
- webhooks;
- API keys ou tokens attribuables;
- service accounts possedes;
- agents/bots lies;
- ressources dont elle est owner;
- inconnues ou limites API.

Ce n'est pas un store graph au depart. C'est une fonction qui part d'un email,
descend aussi loin que les APIs le permettent, et dit explicitement ou elle
s'arrete.

### Decision policy

La plomberie lit l'etat. Le produit decide ce qui peut changer sans casser.

Sorties attendues:

- `remove_safe`;
- `suspend_safe`;
- `transfer_required`;
- `owner_required`;
- `approval_required`;
- `manual_task_required`;
- `unsupported_by_provider`;
- `unknown_dependency`;
- `never_revoke_first`.

Cette logique doit etre une fonction pure, comme `ClassifySeats` puis
`Reconcile`, avant d'etre une queue d'execution generique.

Cycle de vie obligatoire d'une decision:

```text
proposed -> approved -> executed -> verified
proposed -> rejected
proposed -> blocked
approved -> stale
executed -> verification_failed
```

Champs minimum:

```text
Decision
  id
  subject
  object
  provider
  action_class
  recommended_action
  status
  risk
  reason
  policy_version
  idempotency_key
  required_evidence
  blocked_by
  approved_by
  expires_at
```

Une decision devient `stale` apres un re-scan qui change son objet, son statut,
son owner, son provider capability, ou la version de policy appliquee.

### Action classes et verification provider

`CanRemove` est trop grossier pour Enforce. Il peut signifier supprimer,
suspendre, desactiver, retirer d'une org, ou simplement masquer un utilisateur.

La roadmap doit converger vers une matrice:

| Action class | Effet attendu | Verification minimale |
|---|---|---|
| `delete_user` | compte supprime | absent au re-scan provider |
| `deactivate_user` | login bloque, objet conserve | statut provider inactif |
| `suspend_login` | authentification bloquee | statut suspendu + tentative impossible si API exposee |
| `remove_workspace_member` | plus membre de l'espace | absent de l'org/workspace |
| `release_paid_seat` | seat non facture | billed seats diminue ou provider confirme release |
| `transfer_ownership` | owner change | owner actuel expose par API |
| `request_owner_attestation` | humain responsable declare | approbation signee |

Enforce ne doit pas dependre de `CanRemove` seul. Il depend de:

- action class;
- provider;
- verification live du comportement;
- preuve before/after;
- historique d'approbations sans modification;
- blast radius.

### Evidence

Chaque action ou decision produit un certificat partiel. L'offboarding complet
assemble ces certificats.

```text
OffboardingCertificate
  subject
  trigger
  mode
  started_at
  completed_at
  closed_access[]
  transferred_responsibility[]
  pending_reviews[]
  unsupported_actions[]
  unknowns[]
  evidence[]
```

Le certificat doit dire aussi bien:

- "GitHub account removed";
- "Linear account suspended";
- "GitHub App found, creator unknown because audit log unavailable";
- "Credential has no owner; owner attestation requested";
- "Provider API cannot transfer this resource";
- "Rechecked at J+7, access still absent".

Statuts du certificat:

| Statut | Sens |
|---|---|
| `complete` | tous les objets trouves sont fermes, transferes, ou explicitement approuves |
| `complete_with_provider_limits` | complet selon les APIs disponibles, avec limites explicites |
| `blocked` | une action requise attend une personne, un scope API, ou un provider |
| `incomplete` | au moins un acces connu reste ouvert sans decision |
| `stale` | l'etat a change depuis la derniere verification |

Un certificat "incomplet mais honnete" n'est pas un echec. C'est vendable si le
produit dit clairement pourquoi il est incomplet et quelle action le rendrait
complet.

Champs d'evidence minimum:

```text
EvidenceItem
  id
  source_provider
  source_endpoint
  collected_at
  provider_timestamp
  actor
  scopes_used
  policy_version
  before_snapshot_hash
  after_snapshot_hash
  redaction_summary
  known_limits[]
```

## Axe 1: Offboarding Autopilot

### Role dans le produit

C'est le wedge.

Mais la promesse doit etre reformulee:

> Your last offboarding is not done until the human and non-human leftovers are
> closed, transferred, or explicitly proven unknowable.

Le produit ne doit pas seulement automatiser les suppressions. Il doit finir le
travail.

### Pourquoi c'est important

L'offboarding est repetitif, urgent, anxiogene et deja budgete mentalement par
l'IT. Les entreprises pensent souvent que SSO/SCIM suffit, puis decouvrent:

- comptes non federes;
- invites externes;
- seats suspendus mais payants;
- apps OAuth;
- integrations creees par des personnes parties;
- tokens utilises par la production;
- ownership de ressources;
- providers qui ne permettent pas l'action attendue.

### Urgence

Haute pour l'ICP 150-1000 employes:

- assez de SaaS pour que la checklist soit incomplete;
- assez de departs pour que la douleur revienne;
- assez d'audit pour que la preuve compte;
- pas toujours de plateforme SMP/SSPM complete.

### Valeur debloquee

- temps d'offboarding reduit;
- moins d'acces residuels;
- moins de seats perdus;
- preuve d'audit automatique;
- actions semi-automatiques apres confiance;
- visibilite sur ce que l'IdP ne voit pas.

### Le client serait triste si on l'enleve

Oui, si `offboard` devient:

- la premiere commande lancee lors d'un depart;
- la checklist canonique;
- le certificat donne a Security/Compliance;
- la source des actions Jira/Slack;
- l'endroit ou les leftovers non-humains apparaissent.

Non, si c'est seulement un rapport de plus.

### Questions Mom Test

- "Raconte-moi le dernier offboarding qui n'etait pas trivial."
- "Quelles applications avez-vous verifiees manuellement ?"
- "Qu'est-ce qui a ete trouve apres coup ?"
- "Quel acces est reste volontairement ouvert, et qui l'a approuve ?"
- "Quelle preuve avez-vous gardee ?"
- "Qu'avez-vous deja scripté ou acheté pour ce probleme ?"
- "Avez-vous regarde BetterCloud, Torii ou Zluri ? Qu'est-ce qui vous a arrete ?"

### MVP

```text
unseat offboard <email>
unseat offboard <email> --json
```

Mode Observe seulement.

P1 est volontairement vertical:

- Google Directory comme source humaine;
- GitHub comme premier provider profond;
- identity resolution avec aliases manuels et evidence;
- org members et outside collaborators;
- GitHub Apps / integrations seulement si l'API les expose;
- Copilot billing si le connecteur est configure;
- actions proposees, mais non executables;
- unknowns et unsupported actions explicites;
- certificat local.

Hors P1:

- OAuth generic cross-SaaS;
- secret stores;
- webhooks generic;
- service accounts generic;
- auto-revocation de credentials;
- ownership transfer automatique;
- SaaS hosted complet.

La sortie doit contenir:

- `subject` resolu;
- comptes humains trouves;
- objets non-humains GitHub trouvables;
- actions proposees;
- actions bloquees;
- unknowns;
- statut du certificat;
- evidence locale;
- phrase de demo honnete.

Phrase de demo tenable:

> Alice a quitte la societe il y a 34 jours. 6 comptes SaaS encore actifs,
> 3 tokens, 2 GitHub Apps. Une app a ete installee par Alice; l'autre est
> impossible a attribuer parce que GitHub ne l'expose pas sans audit log
> Enterprise. Aucune n'a de proprietaire declare.

### Risques

- Offboarding workflow est une categorie occupee.
- Les APIs peuvent etre trop faibles pour fermer la boucle.
- Attribution NHI partielle, surtout sans GitHub Enterprise.
- Le declencheur manuel ne suffit pas pour une experience SaaS mature.

### Signal de validation

Le signal fort n'est pas "c'est interessant". C'est:

- le client donne son dernier offboarding pour essai;
- il exporte ou partage le certificat;
- il demande un trigger HRIS;
- il demande "quels providers dois-je connecter pour rendre ce certificat
  complet ?";

## Axe 2: Descendance Numerique, Pas Graphe General

### Role dans le produit

La descendance numerique remplace l'axe "Identity Graph".

Un graph general est une plateforme sans client clair. Une fermeture transitive
depuis une personne est une requete produit:

```text
Que reste-t-il de cette personne dans notre SaaS stack ?
```

### Pourquoi c'est important

Les leftovers ne sont pas seulement des seats. Ils sont des dependances:

- app installee par une personne;
- webhook encore actif;
- secret stocke dans CI;
- service account cree par un ancien employe;
- repo ou workspace sans nouveau owner;
- agent ou bot utilisant un token humain.

### Urgence

Moyenne-haute. Ce n'est pas le premier objet a construire comme infrastructure,
mais c'est la forme de sortie qui rend `offboard` different des workflows
existants.

### Valeur debloquee

- rapport centre sur un depart;
- exploration bornee;
- limites API explicites;
- meilleur input pour decision policy;
- pas besoin d'un store graph premature.

### Le client serait triste si on l'enleve

Oui, si c'est la partie qui revele les leftovers invisibles.

Non, si c'est seulement une visualisation de relations deja connues.

### Questions Mom Test

- "Quand quelqu'un part, comment trouvez-vous ce qu'il possedait ?"
- "Montre-moi ou tu irais chercher les apps OAuth installees par cette personne."
- "La derniere fois qu'un owner est parti, qu'est-ce qui a ete transfere ?"
- "Quelle relation est impossible a reconstruire aujourd'hui ?"

### MVP

Pas de tables `nodes` / `edges`.

Ajouter:

```text
unseat offboard <email> --explain
```

Le code calcule a la demande:

- directory identity;
- aliases;
- provider seats;
- credential creator si connu;
- owner local si declare;
- dependencies unknown.

### Risques

- Profondeur faible sur beaucoup de providers.
- Tentation de construire un graph store trop tot.
- Trop d'inconnues si peu de providers profonds.

### Signal de validation

Le client demande plus de profondeur sur un provider specifique, surtout
GitHub, pas "un joli graphe".

## Axe 3: Decision Policy, Puis Action Engine

### Role dans le produit

La spec initiale mettait "Action Engine" en premier. Correction: ce qui doit
venir tot est la **decision policy**.

Le produit doit savoir dire:

- ceci peut etre supprime;
- ceci doit etre suspendu;
- ceci exige transfert;
- ceci exige owner attestation;
- ceci exige approval;
- ceci ne peut pas etre actionne par l'API;
- ceci ne doit jamais etre revoque en premiere action.

L'action engine generique vient plus tard, quand il existe au moins deux vrais
appelants (`offboard` + `reclaim` ou `nhi review`).

### Pourquoi c'est important

Le coeur produit n'est pas `apply()`. Le coeur produit est "quoi faire sans
casser".

Le repo a deja une version de cette idee pour les seats:

- classification;
- reconciliation;
- grace period;
- dry run;
- pending removals;
- events.

Il faut etendre ce jugement avant d'extraire une abstraction.

### Urgence

Tres haute pour la decision policy. Moyenne pour l'action engine generique.

### Valeur debloquee

- recommandations defensables;
- moins de risques d'automation prematuree;
- separation propre entre jugement et execution;
- base pour Approve/Enforce;
- tests de safety.

### Le client serait triste si on l'enleve

Il ne demandera pas une "decision policy", mais il sera triste si:

- le produit propose des actions dangereuses;
- il ne comprend pas pourquoi une action est bloquee;
- il doit refaire les memes decisions a la main.

### Questions Mom Test

- "Quelle action d'acces avez-vous choisi de ne pas automatiser ?"
- "Que s'est-il passe la fois ou une suppression a casse quelque chose ?"
- "Qui decide qu'un compte peut etre suspendu plutot que supprime ?"
- "Quelles actions sont toujours approuvees ?"

### MVP

Modele pur, aligné avec la section "Decision policy":

```text
Decision
  id
  subject
  object
  provider
  action_class
  recommended_action
  status
  risk
  reason
  policy_version
  idempotency_key
  required_evidence
  blocked_by
```

Premieres decisions:

- remove/suspend human seat;
- transfer responsibility for credential;
- request owner;
- create manual task;
- mark provider unsupported.

Pas de `rollback()` generique. La reversibilite doit etre choisie avant:
suspendre plutot que supprimer, desactiver plutot que revoquer.

### Risques

- Generaliser depuis un seul flux.
- Cacher la logique dans des commandes CLI au lieu de la tester dans `core`.
- Confondre rollback reel et rollback hint.

### Signal de validation

Les decisions rejetees par l'utilisateur ont une raison reutilisable, et les
decisions approuvees se repetent sans modification.

## Axe 4: Non-Human Identity Lifecycle

### Role dans le produit

C'est le differenciateur, pas le wedge initial.

L'entree est l'offboarding humain. La surprise est:

```text
Cette personne a laisse des apps, tokens, webhooks ou agents que votre IdP ne
voit pas.
```

### Pourquoi c'est important

Le nombre d'identites non humaines augmente:

- SaaS interconnectes;
- OAuth grants;
- service accounts;
- CI/CD tokens;
- agents AI;
- bots internes;
- API keys de production.

Elles sont plus dures a supprimer parce que personne ne sait toujours qui en
depend.

### Urgence

Haute comme tendance, mais a livrer prudemment.

Le produit ne doit jamais lire "creator left" comme "safe to revoke".

### Valeur debloquee

- decouverte des leftovers invisibles;
- ownership attestation;
- expiry et reminders;
- workflow de transfert;
- reduction du risque sans provoquer de panne;
- preparation a la gouvernance agents AI.

### Le client serait triste si on l'enleve

Oui, si NHI est la partie qui trouve ce que les outils d'offboarding classiques
ne trouvent pas.

Non, si NHI ne fait que lister des apps sans workflow owner/attestation.

### Questions Mom Test

- "La derniere fois que vous avez supprime une cle ou integration, qu'est-ce qui
  a casse ?"
- "Si rien n'a casse, comment avez-vous su que rien n'en dependait ?"
- "Montre-moi ou tu irais compter les apps OAuth ou service accounts."
- "Le dernier bot ou agent deploye chez vous, qui a cree son token et ou est-il
  stocke ?"
- "Quand un owner part, qui accepte de reprendre la responsabilite ?"

### MVP

Retirer `nhi revoke` du MVP.

MVP:

```text
unseat audit credentials
unseat offboard <email>
unseat decisions list --status proposed
unseat decisions attest-owner <decision-id> --owner <email> --reason "..."
```

Actions:

- owner required;
- purpose required;
- expiry required;
- transfer responsibility;
- manual rotation task.

Revocation seulement plus tard, quand:

- non-usage verifie;
- owner approuve;
- provider permet une action reversible ou rotation avec recouvrement.

### Risques

- Attribution GitHub App faible sans audit log Enterprise.
- Usage credential souvent inconnu.
- Owner attestation peut ressembler a un tableur si elle ne s'integre pas a
  offboarding.
- Le terme NHI peut parler a Security, moins a IT.

### Signal de validation

Le client accepte d'assigner des owners parce que la liste vient d'un vrai depart
ou d'un vrai audit, pas d'une page "inventory" abstraite.

## Axe 5: Billing Actionability

### Role dans le produit

Billing est un signal de tri et de ROI, pas le titre.

Le titre reste access/offboarding completeness. Billing sert a dire:

- ce cleanup recupere probablement de l'argent;
- cette economie est prouvee;
- cette economie est inconnue;
- ce prepaid pool est sous-utilise;
- ce reclaim exige une action procurement.

### Pourquoi c'est important

Le cout rend le probleme urgent et partageable avec Finance. Mais le produit
perd la confiance s'il invente les montants.

### Urgence

Moyenne. Utile pour vendre, mais pas assez couvert par les APIs pour porter la
promesse principale aujourd'hui.

### Valeur debloquee

- prioritisation des actions;
- identification billed > filled;
- evidence de reclaim quand API donne le prix;
- meilleure conversation de renouvellement;
- aucun YAML pricing.

### Le client serait triste si on l'enleve

Oui, si le produit execute des reclaim actions et prouve les economies.

Non, si le billing reste souvent inconnu ou non-actionnable.

### Questions Mom Test

- "Quand avez-vous recupere du SaaS spend pour la derniere fois ?"
- "Comment avez-vous prouve l'economie ?"
- "Quel vendor etait le plus opaque entre facture et usage ?"
- "Qu'avez-vous fait quand billed seats et users actifs ne matchaient pas ?"

### MVP

Dans `offboard` et `scan`, enrichir les actions:

- `seat_count_known`;
- `billed_seat_count_known`;
- `unit_price_known`;
- `contract_unknown`;
- `money_unknown`;
- `renewal_only_saving`;
- `immediate_release_possible`;
- `procurement_required`;
- `no_price_claimed`.

Claims autorises:

| Claim | Condition |
|---|---|
| `saving_verified` | provider expose prix ou montant facture, et le seat est libere |
| `saving_estimated_by_provider` | provider expose un prix public/API, pas un YAML utilisateur |
| `seat_reclaim_verified` | provider confirme que le seat n'est plus assigne |
| `renewal_opportunity` | seats inutilises/prepayes visibles, economie non immediate |
| `money_unknown` | count connu mais prix absent |

Claim interdit: inventer un montant depuis une config manuelle.

Plus tard:

```text
unseat reclaim list
unseat reclaim apply
```

### Risques

- Trois billing providers seulement aujourd'hui.
- Enterprise contracts rendent les prix inconnus.
- Savings au renouvellement, pas immediats.
- "Spend reclaim" attire une comparaison directe avec SMP/procurement tools.

### Signal de validation

Finance ou IT partage le certificat parce qu'il contient une preuve de reclaim,
pas parce qu'il contient un graphique.

## Axe 6: Decisions et Approval Inbox

### Role dans le produit

Couper l'axe en deux:

1. decision ledger tot;
2. inbox plus tard.

Le ledger est necessaire des le mode Approve:

- approve;
- reject;
- reason;
- approver;
- timestamp;
- action class;
- provider;
- evidence.

L'inbox est une surface. Elle devient utile seulement quand il y a assez de
volume.

### Pourquoi c'est important

Les decisions repetitives creent la justification d'Enforce. Sans decision
persisted, Enforce est un acte de foi.

### Urgence

Decision ledger: haute.

Inbox UI/Slack: moyenne, apres validation du volume.

### Valeur debloquee

- batch approval plus tard;
- apprentissage des actions safe;
- preuve d'approbation;
- delegation;
- raison des rejets reutilisable.

### Le client serait triste si on l'enleve

Oui pour le ledger si le produit agit.

Pas forcement pour l'inbox si le volume reste faible et que CLI/Jira suffit.

### Questions Mom Test

- "Quelles decisions d'acces sont en attente aujourd'hui ?"
- "Ou vivent-elles ?"
- "Qui approuve quoi ?"
- "Quelle decision est toujours approuvee ?"
- "Quelle info manque pour approuver vite ?"

### MVP

```text
unseat decisions list
unseat decisions approve <id>
unseat decisions reject <id> --reason
```

Slack/Jira seulement apres.

### Risques

- Construire une inbox avant le volume.
- Dupliquer Jira.
- Generer trop de bruit.

### Signal de validation

Les approbations se repetent avec peu ou pas de modification. Le client demande
auto-approval sur une classe precise.

## Axe 7: Evidence by Default

### Role dans le produit

Ce n'est pas un axe tardif. C'est une propriete de toute action, livree des la
premiere sortie `offboard`.

### Pourquoi c'est important

C'est l'avantage qui s'accumule:

- les connecteurs se copient;
- les rules se copient;
- trois ans de preuves d'acces et de decisions ne se copient pas.

La retention et l'export des preuves sont probablement une meilleure frontiere
payante que le nombre de connecteurs.

### Urgence

Immediat des que `offboard` existe.

### Valeur debloquee

- confiance dans l'automation;
- audit readiness;
- explication des unknowns;
- preuve d'effort quand l'API ne permet pas de savoir;
- switching cost legitime.

### Le client serait triste si on l'enleve

Oui, si le certificat sert aux audits, aux tickets, aux revues internes ou aux
renouvellements.

### Questions Mom Test

- "Lors du dernier audit, quelles preuves d'access removal avez-vous fournies ?"
- "Combien de temps a pris la collecte ?"
- "Quelle preuve etait impossible a reconstruire apres coup ?"
- "Quel format est accepte ?"
- "Avez-vous deja du prouver pourquoi un acces n'a pas ete retire ?"

### MVP

Pour `offboard` Observe:

- store local evidence;
- JSON export;
- human-readable certificate;
- unknowns explicites;
- redaction de secrets.

Commandes futures:

```text
unseat evidence show <id>
unseat evidence export --since 30d
```

### Risques

- Stocker des secrets.
- Produire des logs techniques au lieu de preuves lisibles.
- Retention sans access control.
- Evidence non acceptee par compliance.

### Signal de validation

Le client exporte le certificat sans demande de support et le partage a
Security/Compliance.

## Roadmap Proposee

### Phase 0: Safety et cadrage

1. Garder les providers HRIS BambooHR/Rippling/Deel en read-only: ils ne
   doivent pas redevenir des SaaS targets destructibles sans modele explicite.
2. Introduire la notion de source d'identite RH: termination date, employment
   status, manager, department.
3. Afficher les modes Observe/Approve/Enforce explicitement dans les sorties.
4. Generer un tableau de capabilities depuis le code.
5. Remplacer progressivement `CanRemove` par action classes verifiables.

### Phase 0.5: Fondations SaaS sans hosted complet

1. Ajouter `tenant_id` dans les schemas d'evidence et decision, meme si la
   valeur est locale au debut.
2. Separer secret storage, connector config, et evidence redacted.
3. Introduire `actor`: CLI user, system, scheduler, approver.
4. Rendre chaque action idempotente avant toute queue asynchrone.
5. Preparer export evidence portable: JSON stable + hashes.

### Phase 1: Offboarding Observe

1. `unseat offboard <email>` en Observe.
2. Perimetre strict: Google Directory + GitHub.
3. Identity resolution avec preuve d'association.
4. Descendance numerique GitHub seulement.
5. Decisions non executables mais classees.
6. Certificat local avec statut `complete`, `complete_with_provider_limits`,
   `blocked`, `incomplete`, ou `stale`.
7. Unknowns et unsupported actions explicites.

Objectif demo:

```text
Deux connexions, un resultat en moins de 15 minutes:
Google Directory + GitHub -> certificat de depart incomplet mais honnete.
```

### Phase 2: Approve

1. Decision ledger avec lifecycle complet.
2. Approve/reject avec raison.
3. Expiration/stale apres re-scan.
4. Actions manuelles/tickets pour unsupported.
5. Owner attestation NHI.
6. Re-scan a J+N pour confirmer.

### Phase 3: Profondeur GitHub

Approfondir GitHub avant d'ajouter dix logos:

- org users;
- outside collaborators;
- GitHub Apps;
- Copilot seats;
- deploy keys;
- Actions secrets;
- runners;
- ownership transfer where possible;
- audit log integration;
- evidence.

Raison: GitHub est le seul provider ou la boucle humain -> app/token -> consumer
peut devenir partiellement soluble.

### Phase 4: Enforce perimetre

1. Auto-suspend low-risk seats apres grace period.
2. Jamais Enforce global.
3. Jamais credential revoke en premiere action.
4. Enforce seulement sur providers verifies et action classes deja approuvees.

### Phase 5: Reclaim, Inbox, SaaS hosted

1. `reclaim` comme deuxieme appelant de l'action engine.
2. Extraction action engine generique.
3. Inbox si le volume de decisions le justifie.
4. Surface SaaS non-dashboard: offboarding workbench, decision queue,
   certificate timeline, provider coverage map.
5. SaaS hosted pour scheduling, retention, Slack/Jira, evidence export.

## Packaging et prix

Principes:

- ne pas facturer par connecteur;
- ne pas facturer un pourcentage des economies;
- ne pas facturer principalement au nombre de NHI decouvertes;
- facturer sur humains + mode + retention/evidence.

Structure possible:

| Plan | Mode | Taille | Evidence |
|---|---|---:|---|
| Discovery | Observe | jusqu'a 250 humains | 30 jours local |
| Starter | Observe + Approve | ~250 humains | 1 an |
| Growth | Approve + limited Enforce | ~1000 humains | 3 ans |
| Scale | Enforce perimetre + integrations | custom | retention custom |

Le NHI count peut exister comme fair-use guardrail, pas comme metrique centrale.
Sinon le produit punit le client quand il decouvre plus de choses.

## Entretiens clients

### Conversation 1: dernier offboarding

- "Raconte-moi le dernier offboarding non trivial."
- "Quelles apps avez-vous verifiees manuellement ?"
- "Qu'est-ce qui a ete trouve apres coup ?"
- "Quelle preuve avez-vous gardee ?"
- "Qui signe que c'est termine ?"
- "Qu'avez-vous deja scripté ou acheté pour ce probleme ?"
- "Avez-vous evalue BetterCloud, Torii, Zluri ou Okta Workflows ? Qu'est-ce qui
  vous a arrete ?"

### Conversation 2: non-human leftovers

- "La derniere fois qu'une cle ou integration a ete supprimee, qu'est-ce qui a
  casse ?"
- "Si vous n'en supprimez jamais, pourquoi ?"
- "Montre-moi ou tu compterais les OAuth apps/service accounts."
- "Le dernier agent/bot deploye: qui a cree son token, ou est-il stocke, qui en
  est owner ?"
- "Quand l'owner part, que faites-vous ?"

### Conversation 3: decisions

- "Quelles decisions d'acces sont en attente cette semaine ?"
- "Ou vivent-elles ?"
- "Qui approuve quoi ?"
- "Quelle decision est toujours approuvee ?"
- "Quelle suppression avez-vous refuse d'automatiser, et pourquoi ?"

### Conversation 4: preuve et audit

- "Lors du dernier audit, quelles preuves d'offboarding avez-vous fournies ?"
- "Combien de temps a pris la collecte ?"
- "Qu'est-ce qui etait impossible a prouver apres coup ?"
- "Quel format a ete accepte ?"

### Conversation 5: spend

- "Quand avez-vous recupere des SaaS seats pour la derniere fois ?"
- "Comment avez-vous prouve l'economie ?"
- "Quel vendor etait opaque entre facture et usage ?"
- "Qu'avez-vous fait quand billed seats et filled seats ne matchaient pas ?"

## Anti-goals

- Ne pas construire un BI dashboard SaaS generique.
- Ne pas construire un graph store avant une requete produit qui l'exige.
- Ne pas demander de prix manuels en YAML.
- Ne pas facturer un pourcentage des economies.
- Ne pas appeler "unused" ce qui est seulement "unknown".
- Ne pas revoquer un credential parce que son createur est parti.
- Ne pas auto-enforce sur un provider non verifie.
- Ne pas cacher les limites API.
- Ne pas compter les logos comme preuve de profondeur.
- Ne pas stocker de secrets dans l'evidence.

## Questions ouvertes et gates

1. ICP: si cinq entretiens 150-1000 confirment le probleme d'offboarding
   incomplet, garder le wedge. Sinon separer segment IT Ops et segment Security.
2. SSO/SCIM: si trois equipes disent "resolu" mais citent tokens/apps comme
   douleur, avancer NHI en Phase 1.5.
3. Trigger: si HRIS termination revient dans trois entretiens, ajouter trigger
   HRIS apres `offboard <email>`, sans rendre HRIS destructible.
4. Provider P1: si GitHub ne donne pas assez de preuve en deux connexions,
   tester Google + Linear comme certificat plus simple.
5. Unknowns: si les clients partagent un certificat avec unknowns sans support,
   garder `complete_with_provider_limits` comme statut vendable.
6. Compliance: si le format JSON + hashes n'est pas accepte, prioriser export
   PDF/CSV signe avant Enforce.
7. Pricing: si retention/evidence n'est pas comprise comme frontiere payante,
   simplifier en humains + mode.
8. GitHub depth: si outside collaborators + apps suffisent a provoquer une
   decision client, reporter deploy keys/secrets/runners.

## Sources marche consultees

- BetterCloud platform: onboarding/offboarding workflow automation and SaaS
  management positioning: https://www.bettercloud.com/platform/
- Torii positioning: SaaS and AI stack discovery, spend reclaim, onboarding and
  offboarding automation: https://www.toriihq.com/
- Zluri access reviews and lifecycle automation:
  https://www.zluri.com/access-reviews
- AppOmni SSPM framing around SaaS posture, third-party integrations and risky
  behavior: https://appomni.com/learn/saas-security-fundamentals/sspm/
- Oasis positioning around AI agents and non-human identities across SaaS, cloud
  and on-prem: https://oasis.security/
- BetterCloud SaaS management best practices noting SaaS risk now includes API
  permissions and OAuth tokens, not only user accounts:
  https://www.bettercloud.com/monitor/best-practices-for-saas-management/
