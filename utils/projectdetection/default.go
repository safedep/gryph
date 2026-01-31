package projectdetection

var defaultRegistry *Registry

func init() {
	defaultRegistry = NewRegistry()
	defaultRegistry.Register(npmDetector{})
	defaultRegistry.Register(cargoDetector{})
	defaultRegistry.Register(gomodDetector{})
	defaultRegistry.Register(pyprojectDetector{})
}
