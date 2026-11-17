package main

// orderedPicksRefusal refuses a test that states the order of simultaneous
// triggers AND card picks for those triggers' searches (Verdant Succession:
// same-text "Whenever ..." orderings dropped, then Birds, Elf, Ankle Biter
// named for those triggers). XMage resolves the triggers in the stated order
// and consumes the picks in it; gorge poses the trigger_order with identical
// labels, keeps its own order, and asks each trigger's search in that order,
// so the picks land on other triggers. The runner cannot state the order, so
// the picks cannot be matched to their triggers.
//
// Only a same-text pair arms this: a single dropped order, or orders of
// different abilities, cannot line the picks up with a repeated search, and
// refusing on that weaker evidence would mask a real assertion failure.
func (t *translator) orderedPicksRefusal() *refusal {
	if t.droppedOrder < 2 || len(t.droppedOrderTexts) < 2 {
		return nil
	}
	first := t.droppedOrderTexts[0]
	for _, text := range t.droppedOrderTexts[1:] {
		if text != first {
			return nil
		}
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
	return refuse("trigger-order-search", "%d same-text trigger orderings dropped, %d card picks follow the stated order", t.droppedOrder, picks)
}
