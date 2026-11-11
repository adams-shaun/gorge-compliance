package main

// syntheticScripts are the synthetic card and token scripts the neutral
// tests compile, keyed by path under a temporary corpus root. They are Go
// strings, written at test time, because no file shaped like a Forge script is
// committed (internal/boundary).
var syntheticScripts = map[string]string{
	"cardsfolder/test_barrens.txt": `Name:Test Barrens
ManaCost:no cost
Types:Land
A:AB$ Mana | Cost$ T | Produced$ C | SpellDescription$ Add {C}.
A:AB$ PutCounter | Cost$ 4 | CounterType$ P1P1 | CounterNum$ 2 | SpellDescription$ Put two +1/+1 counters on CARDNAME.
A:AB$ Mana | Cost$ 1 T | Produced$ Combo W U | SpellDescription$ Add {W} or {U}.
A:AB$ Draw | Cost$ 2 T Sac<1/CARDNAME> | NumCards$ 1 | SpellDescription$ Draw a card.
Oracle:Synthetic test land.
`,
	"cardsfolder/test_blade.txt": `Name:Test Blade
ManaCost:1
Types:Artifact Equipment
K:Equip:2
Oracle:Synthetic test equipment.
`,
	"cardsfolder/test_charm.txt": `Name:Test Charm
ManaCost:R W
Types:Instant
A:SP$ Charm | Choices$ DBLife,DBDraw,DBDamage
SVar:DBLife:DB$ GainLife | LifeAmount$ 3 | SpellDescription$ You gain 3 life.
SVar:DBDraw:DB$ Draw | NumCards$ 1 | SpellDescription$ Draw a card.
SVar:DBDamage:DB$ DealDamage | ValidTgts$ Player | NumDmg$ 2 | SpellDescription$ CARDNAME deals 2 damage to target player.
Oracle:Synthetic test charm.
`,
	"cardsfolder/test_grove.txt": `Name:Test Grove
ManaCost:no cost
Types:Land Forest
Oracle:Synthetic test land.
`,
	"cardsfolder/test_walker.txt": `Name:Test Walker
ManaCost:2 W
Types:Legendary Planeswalker Testwalker
Loyalty:3
A:AB$ GainLife | Cost$ AddCounter<1/LOYALTY> | Planeswalker$ True | LifeAmount$ 2 | SpellDescription$ You gain 2 life.
A:AB$ Draw | Cost$ SubCounter<2/LOYALTY> | Planeswalker$ True | NumCards$ 1 | SpellDescription$ Draw a card.
Oracle:Synthetic test planeswalker.
`,
	"tokenscripts/c_test_clue.txt": `Name:Clue Token
ManaCost:no cost
Types:Artifact Clue
A:AB$ Draw | Cost$ 2 Sac<1/CARDNAME/this token> | NumCards$ 1 | SpellDescription$ Draw a card.
Oracle:Synthetic test clue.\nSecond line.
`,
	"tokenscripts/wu_1_1_test_spirit_flying.txt": `Name:Spirit Token
ManaCost:no cost
Colors:white,blue
Types:Legendary Creature Spirit
PT:1/1
K:Flying
Oracle:Flying
`,
}
