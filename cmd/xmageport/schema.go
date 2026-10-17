package main

// Output types mirroring rules.oracleScenario's JSON schema (rules decodes
// with DisallowUnknownFields, so only fields it knows are written) plus the
// xmageport wrapper (xmage, known_bug).

type portFile struct {
	Source    string         `json:"source"`
	Scenarios []portScenario `json:"scenarios"`
}

type portScenario struct {
	Name     string           `json:"name"`
	Turn     *int             `json:"turn,omitempty"`
	CR       []string         `json:"cr"`
	Why      string           `json:"why"`
	Setup    map[string]*seat `json:"setup"`
	Steps    []*step          `json:"steps"`
	Expect   []*expect        `json:"expect"`
	XMage    string           `json:"xmage"`
	KnownBug string           `json:"known_bug,omitempty"`
}

type seat struct {
	Hand        []string                    `json:"hand,omitempty"`
	Battlefield []string                    `json:"battlefield,omitempty"`
	Tapped      []string                    `json:"tapped,omitempty"`
	Graveyard   []string                    `json:"graveyard,omitempty"`
	Exile       []string                    `json:"exile,omitempty"`
	LibraryTop  []string                    `json:"library_top,omitempty"`
	Life        *int32                      `json:"life,omitempty"`
	Counters    map[string]map[string]int32 `json:"counters,omitempty"`
}

type answer struct {
	Kind string   `json:"kind"`
	Pick []string `json:"pick"`
	// Seat is the XMage choice's own player. rules' runner asserts the
	// decision the answer is consumed by was posed to it, so a decider that
	// resolves to the wrong seat fails the scenario (B8).
	Seat *int `json:"seat,omitempty"`
}

type step struct {
	Op           string      `json:"op"`
	Seat         int         `json:"seat"`
	Card         string      `json:"card,omitempty"`
	Mana         string      `json:"mana,omitempty"`
	Targets      []string    `json:"targets,omitempty"`
	Kicked       bool        `json:"kicked,omitempty"`
	AbilityIndex *int        `json:"ability_index,omitempty"`
	Attackers    []string    `json:"attackers,omitempty"`
	Defender     string      `json:"defender,omitempty"`
	Blocks       [][2]string `json:"blocks,omitempty"`
	Step         string      `json:"step,omitempty"`
	Active       string      `json:"active,omitempty"`
	Decision     string      `json:"decision,omitempty"`
	Answers      []answer    `json:"answers,omitempty"`
	Expect       []*expect   `json:"expect,omitempty"`
	// XAbility is the XMage activateAbility text, kept only for the XMage
	// driver cross-check input (never written to a committed scenario).
	XAbility string `json:"-"`
}

// xans is one original XMage choice, for the driver cross-check.
type xans struct {
	Seat  int    `json:"seat"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type count struct {
	Seat  int    `json:"seat"`
	Zone  string `json:"zone"`
	Name  string `json:"name"`
	Token *bool  `json:"token,omitempty"`
}

type expect struct {
	Card          string           `json:"card,omitempty"`
	PT            string           `json:"pt,omitempty"`
	Keywords      []string         `json:"keywords,omitempty"`
	NoKeywords    []string         `json:"no_keywords,omitempty"`
	Types         []string         `json:"types,omitempty"`
	NoTypes       []string         `json:"no_types,omitempty"`
	Tapped        *bool            `json:"tapped,omitempty"`
	Damage        *int32           `json:"damage,omitempty"`
	Counters      map[string]int32 `json:"counters,omitempty"`
	Life          map[string]int32 `json:"life,omitempty"`
	HandSize      map[string]int   `json:"hand_size,omitempty"`
	GraveyardSize map[string]int   `json:"graveyard_size,omitempty"`
	StackSize     *int             `json:"stack_size,omitempty"`
	Count         *count           `json:"count,omitempty"`
	Eq            *int             `json:"eq,omitempty"`
}
