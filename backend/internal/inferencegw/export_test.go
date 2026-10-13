package inferencegw

// LoadTestCatalog loads virtual models into the engine for tests in the
// inferencegw_test package, which cannot build the unexported snapshot.
func LoadTestCatalog(r *RoutingEngine, vms []VirtualModel) {
	r.LoadVirtualCatalog(newTestSnap(vms, nil))
}
