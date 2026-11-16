package i18n

// N marks s as a msgid where the extractor (internal/i18n/extract) wouldn't find it
// on its own: a message the UI shows that reaches it outside the known sinks. It
// returns s unchanged; the UI translates it.
func N(s string) string { return s }
