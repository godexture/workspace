package resource_test

import (
	"fmt"
	"time"

	"github.com/godexture/godec/resource"
)

// A component reports a minimum request separately from its comparative
// estimate; only the Host-owned grant authorizes runtime allocation.
func ExampleGrant_Satisfies() {
	request := resource.Request{Memory: 4 << 20, Workers: 2, Queue: 8}
	estimate := resource.Estimate{
		CPU:        120,
		Memory:     6 << 20,
		Latency:    2 * time.Millisecond,
		Confidence: resource.BoundedConfidence,
	}
	grant := resource.Grant{Memory: 8 << 20, Workers: 4, Queue: 16}

	fmt.Println(grant.Satisfies(request))
	fmt.Println(estimate.Valid(), estimate.Latency)
	// Output:
	// true
	// true 2ms
}
