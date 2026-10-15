// SPDX-License-Identifier: MPL-2.0
package pubgrub_test

import (
	"cmp"
	"fmt"

	"github.com/qix-dev/pubgrub"
	"github.com/qix-dev/pubgrub/ranges"
)

func ExampleResolve() {
	versions := ranges.Ordered[int]()
	provider := pubgrub.NewOfflineDependencyProvider[string](versions, cmp.Compare[int])
	provider.AddDependencies("root", 1, pubgrub.DependencyConstraints[string, ranges.Ranges[int]]{
		{Package: "web", Versions: versions.HigherThan(1)},
	})
	provider.AddDependencies("web", 1, nil)
	provider.AddDependencies("web", 2, nil)

	solution, err := pubgrub.Resolve(provider, "root", 1)
	if err != nil {
		fmt.Println(err)
		return
	}
	version, _ := solution.Get("web")
	fmt.Println(version)
	// Output: 2
}
