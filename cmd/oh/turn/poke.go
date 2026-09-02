package turn

import "crdx.org/io/agent"

const HarnessPoke agent.Kind = "harness_poke"

const PokeMessage = "The previous response ended without a reply. Carry on from where it stopped."

func PokeEvent() agent.Event {
	return agent.Event{Kind: HarnessPoke}
}

func PokeNotice(event agent.Event) (string, bool) {
	if event.Kind != HarnessPoke {
		return "", false
	}

	return PokeMessage, true
}
