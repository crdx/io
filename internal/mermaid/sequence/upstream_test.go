package sequence

import "crdx.org/io/internal/mermaid/diagram"

func upstreamTestConfig(shouldUseASCII bool) *diagram.Config {
	config := diagram.DefaultConfig()
	config.UseAscii = shouldUseASCII
	return config
}
