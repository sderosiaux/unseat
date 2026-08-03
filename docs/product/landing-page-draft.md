# unseat Landing Page Draft

Working assumption: the full product vision is implemented.

Section order below is the page order. Headings marked `H1`/`H2` are the copy that
ships; everything else is direction.

---

## 1. Hero

H1: **Offboarding is not done when the account is disabled.**

Every departure leaves a trail: SaaS seats, outside collaborators, OAuth apps,
API keys, bots, agents, webhooks, ownership, billing ghosts.

unseat follows the person across your stack, closes what can be closed, asks for
approval where judgment is needed, and hands you a certificate that says what is
done, what is blocked, and what no API will tell you.

Primary CTA: **Generate an offboarding certificate**
Under the button: two connections, a certificate in under 15 minutes.

Secondary CTA: See a real certificate, unknowns included

Hero visual:

```text
Offboarding Certificate

Subject     Alice Martin, 3 identities resolved, 1 ambiguous
Trigger     Google Workspace suspension, 34 days ago
Mode        Approve
Status      complete_with_provider_limits

Access closed                12
Ownership transferred         3
Awaiting an owner             2
Provider cannot act           4
Provider will not say         2
Paid seats released           4

Last verified   2026-08-02 22:14 UTC
Next recheck    2026-08-09
```

Note on the mock: no money in the hero. `saving_verified` needs a provider that
exposes a price, which is a minority of them. Released seats are verifiable
everywhere billing is readable, so that is the number the hero earns.

---

## 2. Problem

H2: **Your IdP closes the front door. The side doors stay open.**

SSO and SCIM handle the accounts they federate. They usually cannot tell you:

- which SaaS accounts exist outside the IdP;
- which external identities belong to the person;
- which GitHub Apps, tokens, bots, and agents they created;
- whether a suspended seat is still billed;
- whether ownership moved to anyone;
- whether a provider refused to expose the truth.

What is left gets handled in Slack threads, a spreadsheet, six admin consoles,
and the hope that nobody audits it. unseat turns that last mile into a workflow
you can hand to someone else.

---

## 3. Product promise

H2: **A departure is finished when every leftover is closed, transferred,
approved, or proven unknowable.**

unseat does not pretend the APIs are better than they are. It keeps facts and
guesses in separate columns:

- access found and access removed;
- responsibility transferred;
- approvals pending;
- actions the provider does not support;
- answers the provider will not give;
- billing impact proven by provider data, never by a price you typed.

The output is not a dashboard. It is a certificate, and it is the thing you keep.

---

## 4. The certificate

H2: **Incomplete and honest beats complete and wrong.**

Every offboarding ends in one of five states. None of them is a spinner.

| Status | What it means |
|---|---|
| `complete` | every object found is closed, transferred, or explicitly approved to stay |
| `complete_with_provider_limits` | complete as far as the APIs go, with the limits named |
| `blocked` | something is waiting on a person, an API scope, or a provider |
| `incomplete` | a known access is still open with no decision on it |
| `stale` | the state moved since the last verification |

A certificate that says `complete_with_provider_limits` and names the four
things GitHub would not answer is more useful than a green check that quietly
skipped them.

Certificates do not freeze. unseat rechecks at J+7 and re-opens the ones whose
world changed:

```text
Rechecked 2026-08-09
  GitHub account      still absent      ok
  Linear seat         still suspended   ok
  Webhook wh_3391     re-enabled        certificate -> stale
```

---

## 5. Identity resolution

H2: **The certificate is only worth what the identity match is worth.**

`alice@company.com`, `alice@gmail.com`, `alice-dev`, `a-smith`, and a GitHub App
installed by someone called Alice are not the same person until something proves
they are. unseat scores every association and shows its work.

| Association | Example | Strength |
|---|---|---|
| Directory primary | Workspace `primaryEmail` | strong |
| Directory alias | Workspace alias | strong |
| Verified provider email | email the provider confirms | strong |
| Declared mapping | alias you set, recorded as evidence | strong, manual |
| Username lookalike | `alice` resembles the email | weak |

Three rules that never bend:

- nothing is removed on a weak match alone;
- an unresolved identity goes to review, never to removal;
- every manual alias appears in the evidence, named and timestamped.

The output says `matched`, `unmatched`, or `ambiguous`. An ambiguous match is a
question for a human, not a silent decision.

---

## 6. Digital descendants

H2: **People leave. Their software keeps running.**

unseat walks outward from one person: accounts, roles, paid seats, outside
collaborations, OAuth apps, webhooks, tokens, service accounts, bots, AI agents,
and the resources they own. Where the APIs stop, it says where it stopped.

Creator is not owner. Owner is not consumer.

A token Alice created two years ago might be the only thing keeping a Finance
job running. Her departure does not make that token deletable. It makes its
ownership unclaimed, which is a different problem with a different fix.

So unseat never revokes a credential because the person who created it left.
It asks who owns it now, what depends on it, and what proof exists, then routes
the answer to a human who can say yes.

---

## 7. How it works

### Observe

Connect your directory and your providers. Nothing is writable yet, and Observe
cannot be made writable.

```bash
unseat offboard alice@company.com
```

You get the accounts, the external collaborators, the non-human leftovers, the
billing signals, the unsupported actions, the unknowns, and the evidence behind
each one.

Nobody types that command forever, so the trigger can move upstream: a Workspace
suspension, an HRIS termination date, a ticket, a webhook, a CSV. An unknown HRIS
status is never read as a departure. It stays active until something proves
otherwise.

Removal is driven by the directory, not by your mappings. An active employee who
is in no mapped group is a review item, not a deletion. An incomplete config
produces an incomplete report, never a mass deprovisioning.

### Approve

unseat proposes the decision and keeps the judgment where it belongs:

- remove this seat;
- suspend rather than delete, because it is reversible;
- transfer this app to a new owner;
- ask Finance to confirm this bot;
- open a manual task, because the API cannot do it;
- hold, because usage is unknown.

Decisions persist with a reason, an approver, a policy version, and a lifecycle:
`proposed -> approved -> executed -> verified`. Approve fourteen of the same
action without editing it, and you have the argument for automating it.

### Enforce

Automation is earned per action class, never granted globally.

```yaml
enforce:
  github:
    suspend_user:
      when: directory_user_suspended
      after: 72h
      require_provider_verified: true
    revoke_credential: never
```

And the audit trail says why it ran:

```text
Executed in Enforce because this exact action class was approved 14 times
without modification, on verified provider github, under rule R.
```

Suspension before deletion, reversibility chosen up front, and credential
revocation that stays off the automatic path.

---

## 8. Coverage

H2: **Ask what we can prove, not how many logos we have.**

A connector that only lists users tells you a person exists somewhere. It cannot
tell you what they left behind. So coverage is published per object and per verb,
including the parts that read `unknown`:

```text
provider: github
  users                read, remove
  outside_collaborators read, remove
  org_billing          read
  copilot_seats        read, release
  apps                 read, transfer
  deploy_keys          read
  actions_secrets      unknown
  ownership_transfer   partial
```

The provider matrix ships on the site and is generated from the code, so it
cannot drift into marketing.

---

## 9. Billing

H2: **Spend is how we sort the work, not how we sell it.**

unseat never asks you to keep prices in YAML. If the provider exposes billing,
unseat reads the API. If it does not, unseat says so and moves on.

Each claim is labelled:

| Claim | Condition |
|---|---|
| `saving_verified` | the provider exposes the amount and the seat was released |
| `seat_reclaim_verified` | the provider confirms the seat is no longer assigned |
| `renewal_opportunity` | prepaid or unused seats are visible, the saving lands at renewal |
| `money_unknown` | the count is known, the price is not |
| `procurement_required` | releasing the seat needs a contract change |

Billed seats above filled seats is a finding on its own. It surfaces prepaid
blocks and plan minimums that the user list alone will never show, and it does
not require anyone to know the price.

There is no line for "estimated savings". An Enterprise contract is unknowable
from the outside, and a plausible number taken into a budget meeting is worse
than a blank.

---

## 10. Evidence

H2: **The answer to "how do you know she's gone?"**

Every decision and every action files its own record:

- source provider and the endpoint or capability used;
- collection time and the provider's own timestamp;
- actor, scopes used, policy version;
- before and after snapshots, hashed;
- approval and reason;
- redaction summary, because secrets are never stored;
- the API limits that applied.

Export it as JSON with hashes, or as a readable certificate for Security,
Compliance, Finance, or the audit that comes asking later.

Connectors get copied. Rules get copied. A retained record of who had access to
what, and who decided otherwise, does not.

---

## 11. Who it's for

H2: **Between 150 and 1000 people, and past the point where a checklist works.**

Enough SaaS that every offboarding misses something. Enough departures that it
keeps happening. Enough audit pressure that proof has value. Not always enough
budget or appetite for a full SaaS management platform.

The buyer is usually Head of IT or IT Ops, the person who runs the offboarding
and signs that it is finished. Security cares about what stays reachable after
someone leaves. Compliance cares about the evidence. Finance cares about the
seats, and shows up second.

If you already run BetterCloud, Torii, Zluri, or Okta Workflows, unseat is not
trying to replace the workflow. It answers the part those tools leave open: what
did the automation fail to reach, and can you prove it.

---

## 12. Pricing

Priced on humans, mode, and how long the evidence lives. Never per connector,
never as a cut of the savings, never per non-human identity found, because that
last one charges you more for looking harder.

| Plan | Mode | Size | Evidence retention |
|---|---|---:|---|
| Discovery | Observe | up to 250 people | 30 days, local |
| Starter | Observe + Approve | ~250 people | 1 year |
| Growth | Approve + scoped Enforce | ~1000 people | 3 years |
| Scale | Scoped Enforce + integrations | custom | custom |

---

## 13. Why unseat

H2: **Built to finish the work, not to display it.**

Most tools in this space hand you a risk score and a chart. unseat starts from an
event you already care about, a person left, and ends with a decision, an action,
and proof. It treats humans and non-human identities as the same lifecycle
problem. It separates what is safe to automate from what needs a name attached.
And when a provider answers nothing, the certificate says so instead of showing
a reassuring zero.

---

## 14. Final CTA

H2: **Run the next departure with proof.**

Connect your directory. Connect one provider. Read the certificate, unknowns and
all.

Primary CTA: **Generate a certificate**
Secondary CTA: See sample output

---

## Visual direction

Build:

- the certificate mock, readable in the first viewport, unknowns visible;
- the status ladder as a horizontal scale, `complete` to `stale`;
- the descendants diagram: person -> accounts, apps, tokens, owned resources ->
  decisions -> evidence;
- terminal capture of `unseat offboard alice@company.com`;
- the Observe / Approve / Enforce matrix, with what each mode can and cannot
  write;
- the provider coverage matrix, object by verb, generated from the code;
- the J+7 recheck panel showing one certificate going `stale`;
- billing states: verified, seat reclaim, money unknown, procurement required.

Do not build:

- a generic SaaS dashboard;
- gradient blobs;
- a savings chart with numbers no API produced;
- a logo wall as the coverage story;
- security fear without the workflow that answers it.

---

## Spec traceability

| Page section | Spec source |
|---|---|
| Hero, 4 | Certificate model and statuses, Axe 7 |
| 2, 3 | These, Axe 1 |
| 5 | Resolution d'identite |
| 6 | Axe 2, Axe 4, principle 4 |
| 7 | Modes produit, decision policy, Axe 3, Axe 6, invariant directory |
| 8 | Realite du code actuel, anti-goal on logos |
| 9 | Axe 5 |
| 10 | Axe 7, evidence fields |
| 11 | ICP et acheteur de depart |
| 12 | Packaging et prix |
