package director

import (
	"github.com/mdmdirector/mdmdirector/ddm"
	"github.com/mdmdirector/mdmdirector/director/metrics"
	"github.com/mdmdirector/mdmdirector/utils"
)

// observeDeclarationWrite records a declaration-write call to KMFDDM
func observeDeclarationWrite(declType, operation string, err error) {
	if !utils.Prometheus() {
		return
	}
	class, subtype := ddm.ClassifyDeclarationType(declType)
	metrics.DDMDeclarationWrites(class, subtype, operation, metrics.ResultFromError(err)).Inc()
}

// observeDeclarationWriteByID records a declaration-write call when only the identifier is available
func observeDeclarationWriteByID(declarationID, operation string, err error) {
	if !utils.Prometheus() {
		return
	}
	class, subtype := ddm.ClassifyDeclarationID(declarationID)
	metrics.DDMDeclarationWrites(class, subtype, operation, metrics.ResultFromError(err)).Inc()
}

// observeSetMembershipChange records a set-declaration membership change to KMFDDM
func observeSetMembershipChange(declarationID, operation string, err error) {
	if !utils.Prometheus() {
		return
	}
	class, subtype := ddm.ClassifyDeclarationID(declarationID)
	metrics.DDMSetMembershipChanges(class, subtype, operation, metrics.ResultFromError(err)).Inc()
}

// observeDDMNotify records a KMFDDM enrollment notify call
func observeDDMNotify(err error) {
	if !utils.Prometheus() {
		return
	}
	metrics.DDMNotify(metrics.ResultFromError(err)).Inc()
}
