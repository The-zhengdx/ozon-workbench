package biz

import "regexp"

// secretRE redacts credential assignments before they are written to the session timeline.
// It targets API keys, client ids and authorization headers, not ordinary product text.
var secretRE = regexp.MustCompile(`(?i)((?:api[ _-]?key|authorization|client[ _-]?id|api_key_ciphertext|access_token)\s*["']?\s*[:=]\s*["']?(?:bearer\s+)?)([^\s"',}]+)`)

// Redact masks secret-looking assignments. The surrounding label is kept so the timeline
// still shows that a credential was present.
func Redact(s string) string {
	return secretRE.ReplaceAllString(s, "${1}***")
}
