package main

import (
	"strings"
	"testing"
)

// Everything here is invented: card names, oracle ids and ruling comments.
const fixtureCards = `[
 {"oracle_id":"id-a","name":"Quillback Tinker"},
 {"oracle_id":"id-b","name":"Marrowfen Warden"},
 {"oracle_id":"id-c","name":"Exile"},
 {"oracle_id":"id-d","name":"Twin Lantern // Dark Lantern"}
]`

const fixtureRulings = `[
 {"oracle_id":"id-a","published_at":"2020-01-02","comment":"Alpha beta Marrowfen Warden gamma."},
 {"oracle_id":"id-b","published_at":"2021-03-04","comment":"Marrowfen Warden only mentions itself here."},
 {"oracle_id":"id-a","published_at":"2022-05-06","comment":"Delta exile epsilon."}
]`

func TestCrossCardRulingsFound(t *testing.T) {
	idx, err := loadCards(strings.NewReader(fixtureCards))
	if err != nil {
		t.Fatal(err)
	}
	pairs, st, err := findPairs(strings.NewReader(fixtureRulings), idx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 {
		t.Fatalf("want 1 pair, got %d: %+v", len(pairs), pairs)
	}
	p := pairs[0]
	if p.Ruled != "Quillback Tinker" || p.Second != "Marrowfen Warden" || p.Date != "2020-01-02" {
		t.Errorf("wrong pair: %+v", p)
	}
	if st.Rulings != 3 {
		t.Errorf("rulings scanned = %d, want 3", st.Rulings)
	}
}

func TestFaceNameAndWholePhrase(t *testing.T) {
	idx, err := loadCards(strings.NewReader(fixtureCards))
	if err != nil {
		t.Fatal(err)
	}
	rs := `[
 {"oracle_id":"id-a","published_at":"2020-01-02","comment":"See Dark Lantern here."},
 {"oracle_id":"id-a","published_at":"2020-01-03","comment":"Marrowfen Wardens are not the card."}
]`
	pairs, _, err := findPairs(strings.NewReader(rs), idx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 || pairs[0].Second != "Twin Lantern // Dark Lantern" {
		t.Fatalf("want the DFC pair only, got %+v", pairs)
	}
}

func TestLongestMatchWinsAndDedupes(t *testing.T) {
	idx, err := loadCards(strings.NewReader(`[
 {"oracle_id":"x1","name":"Ember Gate"},
 {"oracle_id":"x2","name":"Ember Gate Keeper"},
 {"oracle_id":"x3","name":"Gate Keeper Prime"}
]`))
	if err != nil {
		t.Fatal(err)
	}
	rs := `[
 {"oracle_id":"x1","published_at":"2019-01-01","comment":"Ember Gate Keeper and Ember Gate Keeper."}
]`
	pairs, _, err := findPairs(strings.NewReader(rs), idx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 || pairs[0].Second != "Ember Gate Keeper" {
		t.Fatalf("want one Ember Gate Keeper pair, got %+v", pairs)
	}
}

func TestJSONLinesAccepted(t *testing.T) {
	idx, err := loadCards(strings.NewReader(`{"oracle_id":"j1","name":"Quillback Tinker"}
{"oracle_id":"j2","name":"Marrowfen Warden"}
`))
	if err != nil {
		t.Fatal(err)
	}
	pairs, _, err := findPairs(strings.NewReader(`{"oracle_id":"j1","published_at":"2018-01-01","comment":"With Marrowfen Warden."}
`), idx)
	if err != nil || len(pairs) != 1 {
		t.Fatalf("pairs=%+v err=%v", pairs, err)
	}
}

func TestBoilerplateCommentDropped(t *testing.T) {
	idx, err := loadCards(strings.NewReader(fixtureCards))
	if err != nil {
		t.Fatal(err)
	}
	same := "Template note that mentions Marrowfen Warden."
	rs := `[
 {"oracle_id":"id-a","published_at":"2020-01-02","comment":"` + same + `"},
 {"oracle_id":"id-d","published_at":"2020-01-02","comment":"` + same + `"},
 {"oracle_id":"id-c","published_at":"2020-01-02","comment":"` + same + `"},
 {"oracle_id":"id-a","published_at":"2021-01-02","comment":"A one-off about Marrowfen Warden."}
]`
	pairs, st, err := findPairs(strings.NewReader(rs), idx)
	if err != nil {
		t.Fatal(err)
	}
	if len(pairs) != 1 || pairs[0].Date != "2021-01-02" || st.Boilerplate != 3 {
		t.Fatalf("pairs=%+v stats=%+v", pairs, st)
	}
}

func TestMatchIsCaseSensitive(t *testing.T) {
	idx, err := loadCards(strings.NewReader(fixtureCards))
	if err != nil {
		t.Fatal(err)
	}
	pairs, _, err := findPairs(strings.NewReader(`[{"oracle_id":"id-a","published_at":"2020-01-02","comment":"the marrowfen warden, lower case, is prose."}]`), idx)
	if err != nil || len(pairs) != 0 {
		t.Fatalf("pairs=%+v err=%v", pairs, err)
	}
}
