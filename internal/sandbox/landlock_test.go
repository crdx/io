package sandbox

import "testing"

func TestUnixSocketsAreIsolatedWhereLandlockCanEnforceIt(t *testing.T) {
	old := configuredRuleset(unixSocketsABI - 1)
	if old.handledAccessFS&accessResolveUnix != 0 || old.scopedRestrictions&scopeAbstractUnix != 0 {
		t.Error("an older ABI was configured with unsupported Unix socket isolation")
	}

	current := configuredRuleset(unixSocketsABI)
	if current.handledAccessFS&accessResolveUnix == 0 {
		t.Error("pathname Unix sockets were not isolated")
	}
	if current.scopedRestrictions&scopeAbstractUnix == 0 {
		t.Error("abstract Unix sockets were not isolated")
	}

	if versionedRights(rightsWrite, unixSocketsABI-1, true)&accessResolveUnix != 0 {
		t.Error("an older ABI granted an unsupported Unix socket right")
	}
	if versionedRights(rightsWrite, unixSocketsABI, true)&accessResolveUnix == 0 {
		t.Error("background mode did not grant Unix socket resolution")
	}
	if versionedRights(rightsWrite, unixSocketsABI, false)&accessResolveUnix != 0 {
		t.Error("a foreground-only policy granted Unix socket resolution")
	}
	if versionedRights(rightsRead, unixSocketsABI, true)&accessResolveUnix != 0 {
		t.Error("a read-only directory granted Unix socket resolution")
	}
}
