# Upstream drafts

Defects the cross-engine search (xengine farm, xproof, cardc) found in XMage and Forge, re-verified on 2026-11-24
against upstream HEAD: magefree/mage master `2a96b0ac12`, Card-Forge/forge master `9a91120cac`. Each still-open
row has a draft issue here and a mirror branch on our fork carrying only that fix, off upstream HEAD. Nothing is
posted upstream; the operator reviews each draft and opens the issue and PR. Rows are in order of how many
`oracle/xengine/diagnosed.tsv` rows each explains.

Forge drafts carry no Forge source or script text (GPL boundary): file:line, the fix in prose, and the mirror commit.

| Engine | Card(s) | Rows | Status | Draft | Mirror branch (our fork) |
|---|---|---|---|---|---|
| XMage | Biorganic Carapace (Spider-Bot, Wraith, Venomized Cat, SP//dr, Oscorp Research Team, Living Brain, Doc Ock's Henchmen, Daily Bugle Reporters) | 9 | still-open | [xmage-biorganic-carapace.md](xmage-biorganic-carapace.md) | `adams-shaun/mage:upstream/biorganic-carapace` |
| XMage | Fight: Plow Through, Kapow!, Chelonian Tackle, Pitiless Fists | 8 | still-open | [xmage-fight-full-power-damage.md](xmage-fight-full-power-damage.md) | `adams-shaun/mage:upstream/fight-full-power-damage` |
| Forge | Myriad: Broodbirth Viper, Dalek Squadron, Cybermen Squadron, Containment Construct, Metalwork Colossus, Mister Gutsy | 6 | still-open | [forge-myriad-no-tokens.md](forge-myriad-no-tokens.md) | `adams-shaun/forge:upstream/myriad-empty-end-of-combat` (push pending) |
| Forge | Turn the Earth | 1 of 5 | still-open | [forge-turn-the-earth.md](forge-turn-the-earth.md) | `adams-shaun/forge:upstream/changezone-shuffle-no-cards` (push pending) |
| Forge | Loaming Shaman, Krosan Reclamation, Gaea's Blessing, Dwell on the Past | 4 of 5 | misdiagnosed: Forge is right ("target player shuffles" even with no card chosen, per the Gatherer rulings); gorge and XMage are at fault | none | none |
| XMage | Valiant: Repel Calamity, High Stride, Crumb and Get It | 3 | still-open | [xmage-valiant-own-targeting-only.md](xmage-valiant-own-targeting-only.md) | `adams-shaun/mage:upstream/valiant-own-targeting-only` |
| XMage | Hungry Ghoul + Reassembling Skeleton | 2 | still-open | [xmage-return-source-graveyard-zcc.md](xmage-return-source-graveyard-zcc.md) | `adams-shaun/mage:upstream/return-source-graveyard-zcc` |
| XMage | Shields Up! on Romulan Bird-of-Prey | 2 | still-open | [xmage-shields-up-station-creature.md](xmage-shields-up-station-creature.md) | `adams-shaun/mage:upstream/shields-up-station-creature` |
| XMage | Pain 101 (AddContinuousEffectToGame target zcc) | 2 | still-open | [xmage-continuous-effect-copied-target-zcc.md](xmage-continuous-effect-copied-target-zcc.md) | `adams-shaun/mage:upstream/continuous-effect-copied-target-zcc` |
| Forge | Nihil Spellbomb (also Declaration in Stone) | 2 | still-open | [forge-nihil-spellbomb.md](forge-nihil-spellbomb.md) | `adams-shaun/forge:upstream/nihil-spellbomb-stray-shuffle` (push pending) |
| Forge | Barkform Harvester (Changeling under ability loss) | 2 | still-open | [forge-barkform-harvester.md](forge-barkform-harvester.md) | `adams-shaun/forge:upstream/changeling-kept-under-ability-loss` (push pending) |
| XMage | Illumination | 1 | still-open | [xmage-illumination-lki-spell.md](xmage-illumination-lki-spell.md) | `adams-shaun/mage:upstream/illumination-lki-spell` |
| XMage | Hellfire | 1 | still-open | [xmage-hellfire-zero-x-damage.md](xmage-hellfire-zero-x-damage.md) | `adams-shaun/mage:upstream/hellfire-zero-x-damage` |
| XMage | Grand Nagus Zek (also Battle-Scarred Survivalist, Morbid Opportunist) | 1 | still-open | [xmage-trigger-limit-lki-zcc.md](xmage-trigger-limit-lki-zcc.md) | `adams-shaun/mage:upstream/trigger-limit-lki-zcc` |
| Forge | Wakanda Forever! (`Shuffle$ False` read as true) | 1 | still-open | [forge-wakanda-forever.md](forge-wakanda-forever.md) | `adams-shaun/forge:upstream/changezoneall-shuffle-false` (push pending) |
| Forge | Parker Luck (row attributed to Sinister Hideout) | 1 | still-open | [forge-parker-luck.md](forge-parker-luck.md) | `adams-shaun/forge:upstream/peekandreveal-clamp` (push pending) |
| Forge | Emrakul's Messenger (Devoid under ability loss) | 1 | still-open | [forge-emrakuls-messenger.md](forge-emrakuls-messenger.md) | `adams-shaun/forge:upstream/devoid-colour-kept-under-ability-loss` (push pending; stacked on the Changeling commit) |
| Forge | Settle the Wreckage (combat damage step priority) | 1 | already reported: Card-Forge/forge#12590 and PR #12722, both open; still reproduces at `9a91120cac` | none | none |
| Forge | Homesickness | 1 | fixed upstream at `1c9b258b93` (#12725); already in FORGE_REF `ff076d4432` | none | none |
| XMage | Chandra, Acolyte of Flame (also Firecat Blitz: same per-token delayed trigger, unfixed) | 0 (fixed on the oracle pin; it explained 4 xproof items) | not yet re-verified against upstream HEAD; fixed on our fork pin | [xmage-chandra-acolyte-one-delayed.md](xmage-chandra-acolyte-one-delayed.md) | `adams-shaun/mage:fix/chandra-acolyte-one-delayed` (fork branch, not an upstream-HEAD mirror) |
| XMage | Extraordinary Journey (X = 0) | 4 xproof items | not yet re-verified against upstream HEAD; fixed on our fork pin | [xmage-extraordinary-journey-x-zero.md](xmage-extraordinary-journey-x-zero.md) | `adams-shaun/mage:fix/extraordinary-journey-x-zero` (fork branch, not an upstream-HEAD mirror) |
| XMage | Thought Collapse, Didn't Say Please (counter before the controller mills) | 4 xproof items | not yet re-verified against upstream HEAD; fixed on our fork pin | [xmage-thought-collapse-counter-first.md](xmage-thought-collapse-counter-first.md) | `adams-shaun/mage:fix/thought-collapse-counter-first` (fork branch, not an upstream-HEAD mirror) |
| Forge | Vivien Reid (also Sigarda, Champion of Light; Sarkhan's Dragonfire); cardc ledger | cardc | still-open | [forge-vivien-reid.md](forge-vivien-reid.md) | `adams-shaun/forge:upstream/dig-rest-random-order` (push pending) |

"Push pending": the Forge mirror branches are committed in `~/projects/forge-oracle` but not on GitHub. Upstream
master adds a `.github/workflows/` file, and the push token lacks the `workflow` scope.

XMage tests ran red then green by shadow-compiling the changed classes and the test over a 1.4.63 build of our pin
`33704ee4fe`. Fight's `PermanentImpl` did not compile against that build at master, so it ran on the pin's copy of
the file; the hunk is the same at master.
