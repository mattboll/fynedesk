// Package desktops implements a virtual desktop pager module for switching and managing multiple workspaces.
package desktops

import "fyshos.com/fynedesk"

func init() {
	fynedesk.RegisterModule(desksMeta)
}
