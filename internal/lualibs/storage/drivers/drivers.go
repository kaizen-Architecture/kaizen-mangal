// multiple storage engines
package drivers

import (
	interfaces "github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/storage/drivers/interfaces"
	memory "github.com/kaizen-architecture/kaizen-mangal/internal/lualibs/storage/drivers/memory"
)

var (
	knownDrivers = make(map[string]interfaces.Driver, 0)
)

func init() {
	knownDrivers[`memory`] = &memory.Storage{}
}

func Get(name string) (interfaces.Driver, bool) {
	// read only struct
	d, ok := knownDrivers[name]
	return d, ok
}
