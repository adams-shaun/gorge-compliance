package main

import (
	"regexp"
	"strconv"
	"strings"
)

// An attack restriction (Crawlspace, Judoon Enforcers, Mirri, Weatherlight
// Duelist: "no more than N creatures can attack you each combat") makes
// gorge refuse a declaration that XMage's test player merely trims: XMage
// declares the attack() calls one at a time and silently drops the ones the
// rules forbid (the Java comment says "can't attack"), where gorge refuses
// the whole declaration and reverses it to the empty one (CR 733.1). The
// translator cannot know which attackers a restriction will drop, but the
// run's transcript names the refused one, so the runner drops it from the
// attack step and plays again until the declaration stands.
var (
	reAttackRestrictRefused = regexp.MustCompile(`^\s+\[attack\] refused, reversed to the empty declaration \(CR 733\.1\): harness: submit attackers \[[\d ]*\]: (?:choices (\d+) and (\d+) are mutually exclusive|choice (\d+) exceeds the per-group limit of \d+) \(group "attack-restrict:\d+(?::\d+)?"\) \((.*)\)$`)
	reAttackOption          = regexp.MustCompile(`\[(\d+) attacker "Attack with (.*?) at [^"]*"`)
)

// trimRestrictedAttack drops from sc's attack steps the attacker the
// transcript shows gorge refusing under an attack-restrict group, and reports
// whether it dropped one. Each call drops at most one attacker per refused
// declaration; the caller replays until none is refused.
func trimRestrictedAttack(sc *portScenario, transcript []string) bool {
	changed := false
	idx := -1
	for _, l := range transcript {
		if strings.HasPrefix(l, "step ") {
			idx++
			continue
		}
		m := reAttackRestrictRefused.FindStringSubmatch(l)
		if m == nil || idx < 0 || idx >= len(sc.Steps) || sc.Steps[idx].Op != "attack" {
			continue
		}
		drop := -1
		switch {
		case m[3] != "":
			drop, _ = strconv.Atoi(m[3])
		default:
			// The engine names the group's first-selected option and the option
			// that exceeded the cap, in the order the attack step declared them:
			// the second number is the later attack() call, which is the one
			// XMage drops. It is not necessarily the larger option index.
			drop, _ = strconv.Atoi(m[2])
		}
		name := ""
		for _, o := range reAttackOption.FindAllStringSubmatch(m[4], -1) {
			if n, _ := strconv.Atoi(o[1]); n == drop {
				name = o[2]
			}
		}
		if name == "" {
			continue
		}
		s := sc.Steps[idx]
		for i := len(s.Attackers) - 1; i >= 0; i-- {
			if attackerName(s.Attackers[i]) == name {
				s.Attackers = append(s.Attackers[:i:i], s.Attackers[i+1:]...)
				changed = true
				break
			}
		}
	}
	return changed
}

// attackerName is the card name of a step reference ("p0:Grizzly Bears#2").
func attackerName(ref string) string {
	if _, n, ok := strings.Cut(ref, ":"); ok {
		ref = n
	}
	if i := strings.LastIndex(ref, "#"); i >= 0 {
		if _, err := strconv.Atoi(ref[i+1:]); err == nil {
			ref = ref[:i]
		}
	}
	return ref
}

// readAheadLabel maps XMage's chapter answer to read ahead ("X=2", which the
// translator keeps as the bare pick "2") to the label of gorge's chapter ask:
// when every offered option is a "readahead" one, the n-th option is chapter n.
func readAheadLabel(pick string, opts []dumpOption) (string, bool) {
	if len(opts) == 0 {
		return "", false
	}
	for _, o := range opts {
		if o.Kind != "readahead" {
			return "", false
		}
	}
	n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(pick, "X=")))
	if err != nil || n < 1 || n > len(opts) {
		return "", false
	}
	return opts[n-1].Label, true
}
