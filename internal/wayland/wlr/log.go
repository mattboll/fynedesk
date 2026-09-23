package wlr

/*
#include <wlr/util/log.h>
*/
import "C"

// LogImportance mirrors enum wlr_log_importance.
type LogImportance uint32

const (
	Silent LogImportance = C.WLR_SILENT
	Error  LogImportance = C.WLR_ERROR
	Info   LogImportance = C.WLR_INFO
	Debug  LogImportance = C.WLR_DEBUG
)

// InitLog sets the wlroots log verbosity. Messages go to stderr.
func InitLog(verbosity LogImportance) {
	C.wlr_log_init(C.enum_wlr_log_importance(verbosity), nil)
}
