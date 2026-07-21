package service

// BeautifyStatus reports the actual runtime capability, rather than only the
// requested environment configuration. It is intentionally read-only so it can
// be exposed from readiness checks and task diagnostics.
func BeautifyStatus() map[string]interface{} {
	if beautifyService == nil {
		return map[string]interface{}{
			"enabled": false,
			"healthy": false,
		}
	}
	return beautifyService.GetStats()
}
