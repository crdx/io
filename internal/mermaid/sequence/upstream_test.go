package sequence

import "crdx.org/io/internal/mermaid/diagram"

func upstreamTestConfig(useASCII bool) *diagram.Config {
	config := diagram.DefaultConfig()
	config.UseAscii = useASCII
	return config
}
