// Package live wires multi-cloud provider factories into the registry.
//
// The FinOps engine never hard-codes a closed list of CSPs. Any cloud registers:
//
//	func init() {
//	    registry.Register("mycloud", func(cfg *config.Config, mode string) (provider.Scanner, provider.Remediator, error) {
//	        return mycloud.NewScanner(...), mycloud.NewRemediator(...), nil
//	    })
//	}
//
// Then enable it in config under providers.mycloud.enabled.
//
// For CSPs without a Go SDK adapter, use the generic inventory provider via
// providers.<id>.options.inventory_file (see configs/inventory.example.yaml).
package live
