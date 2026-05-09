package bff

// Plugin is a module that registers routes on the BFF router.
//
// Each plugin provides a namespace (e.g. "dashboard", "schmutz", "monitoring")
// and a description. The app discovers which plugins are loaded via /api/plugins.
//
// To build a plugin:
//
//	type MyPlugin struct{}
//
//	func (p *MyPlugin) Name() string        { return "my-plugin" }
//	func (p *MyPlugin) Description() string { return "Does cool stuff" }
//	func (p *MyPlugin) Register(r *Router)  { r.Handle("GET /api/my-thing", ...) }
type Plugin interface {
	Name() string
	Description() string
	Register(r *Router)
}

// PluginInfo is the JSON-serializable metadata for a loaded plugin.
type PluginInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}
