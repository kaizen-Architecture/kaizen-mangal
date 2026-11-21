package provider

const CustomProviderExtension = ".lua"

// builtinProviders is intentionally empty starting in v1.2.0 (RFC 0001 Phase 8):
// kaizen-mangal operates exclusively on custom Lua scrapers.
var builtinProviders = []*Provider{}
