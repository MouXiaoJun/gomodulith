package modulith_test

import (
	"context"
	"fmt"

	"github.com/MouXiaoJun/gomodulith/modulith"
)

// This example demonstrates convention-based discovery followed by
// verification, the way it would appear in an architecture test.
//
// The example does not compile a real module graph (there is no Go module
// here), so it builds the application model explicitly to show the API shape.
func ExampleApplication_Verify() {
	app := modulith.New().
		Module("user", "./internal/user/...").
		Module("order", "./internal/order/...").
		Module("payment", "./internal/payment/...")

	app.ModuleRules("order").AllowDependencies("user", "payment")
	app.ModuleRules("payment").AllowDependencies("user")

	fmt.Printf("modules declared: %d\n", len(app.Modules()))
	// Output: modules declared: 3
}

// ExampleLoad shows how a codebase following the default convention
// (internal/<module>/...) is loaded and inspected without any explicit rules.
func ExampleLoad() {
	// In a real project you would call:
	//
	//   app, err := modulith.Load(context.Background(), "./...")
	//
	// and then inspect app.Modules() or run app.Verify().
	app, err := modulith.Load(context.Background(), "./...")
	if err != nil {
		panic(err)
	}
	fmt.Printf("loaded %d package(s), %d module(s)\n", len(app.Packages()), len(app.Modules()))
	// Output: loaded 1 package(s), 0 module(s)
}
