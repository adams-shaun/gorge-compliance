package mtgish

import (
	"reflect"
	"testing"

	"github.com/adams-shaun/gorge/engine/cards"
)

func TestP2KTokens(t *testing.T) {
	if got := gorgeStaticEffects("ForetellAnyTurn", map[string]string{"ValidPlayer": "You"}); !reflect.DeepEqual(got, []string{"player:foretellonanyplayersturn"}) {
		t.Errorf("ForetellAnyTurn: got %v", got)
	}
	if got := extAPI["Assimilate"](&cards.SA{API: "Assimilate"}, nil); !reflect.DeepEqual(got, []string{"assimilate"}) {
		t.Errorf("Assimilate: got %v", got)
	}
	e := &effects{}
	extAction["Assimilate"](e, node{})
	if !reflect.DeepEqual(e.effects, []string{"assimilate"}) {
		t.Errorf("Action.Assimilate: got %v", e.effects)
	}
}
