// Package launcher provides launch suggestion modules including calculator, URL detection, and unit conversion.
package launcher

import "fyshos.com/tyde"

func init() {
	tyde.RegisterModule(calcMeta)
	tyde.RegisterModule(largeTypeMeta)
	tyde.RegisterModule(searchMeta)
	tyde.RegisterModule(urlMeta)
	tyde.RegisterModule(unytsMeta)
	tyde.RegisterModule(qrMeta)
}
