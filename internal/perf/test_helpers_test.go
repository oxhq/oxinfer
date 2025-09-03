package perf

// Helper pointer constructors used across perf tests (kept lightweight).
func intPtr(i int) *int       { return &i }
func boolPtr(b bool) *bool    { return &b }
func stringPtr(s string) *string { return &s }

