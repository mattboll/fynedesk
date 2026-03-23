// Package launcher provides launch suggestion modules including calculator, URL detection, and unit conversion.
package launcher

import "fyshos.com/fynedesk"

func init() {
	fynedesk.RegisterModule(calcMeta)
	fynedesk.RegisterModule(urlMeta)
	fynedesk.RegisterModule(unytsMeta)
}
