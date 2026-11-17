package main

// orderedPicksRefusal refuses a test that states the order of simultaneous
// triggers AND card picks for those triggers' searches (Verdant Succession:
// three "Whenever ..." orderings dropped, then Birds, Elf, Ankle Biter named
// for three same-text triggers). XMage resolves the triggers in the stated
// order and consumes the picks in it; gorge poses the trigger_order with
// identical labels, keeps its own order, and asks each trigger's search in
// that order, so the picks land on other triggers. The runner cannot state
// the order, so the picks cannot be matched to their triggers.
func (t *translator) orderedPicksRefusal() *refusal {
	if t.droppedOrder == 0 {
		return nil
	}
	picks := 0
	for _, ch := range t.choiceList {
		if ch.target {
			picks++
		}
	}
	if picks < 2 {
		return nil
	}
	return refuse("trigger-order-search", "%d trigger orderings dropped, %d card picks follow the stated order", t.droppedOrder, picks)
}
