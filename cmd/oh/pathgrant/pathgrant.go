package pathgrant

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"crdx.org/io/agent"
	"crdx.org/io/cmd/oh/access"
	"crdx.org/io/cmd/oh/shell"
	"crdx.org/io/internal/util/pathutil"
)

type Access string

const (
	ReadAccess  Access = "read"
	WriteAccess Access = "write"
)

type Grant struct {
	Path   string `json:"path"`
	Access Access `json:"access"`
}

type RestoreFailure struct {
	Grant Grant
	Err   error
}

type RestoreResult struct {
	Failures []RestoreFailure
}

type Grants struct {
	workspaceDir string
	pathAccess   *shell.PathAccess
	state        *access.State[[]Grant]
	mutex        sync.Mutex
}

func New(workspaceDir string, pathAccess *shell.PathAccess) *Grants {
	return &Grants{
		workspaceDir: workspaceDir,
		pathAccess:   pathAccess,
		state:        access.New([]Grant(nil), definition()),
	}
}

func NewRestored(workspaceDir string, pathAccess *shell.PathAccess, recordedGrants []Grant) (*Grants, RestoreResult) {
	current := make([]Grant, 0, len(recordedGrants))
	result := RestoreResult{}
	for _, grant := range canonicalGrants(recordedGrants) {
		var err error
		if !filepath.IsAbs(grant.Path) || filepath.Clean(grant.Path) != grant.Path {
			err = fmt.Errorf("invalid recorded path %q", grant.Path)
		} else if grant.Access != ReadAccess && grant.Access != WriteAccess {
			err = fmt.Errorf("invalid recorded access %q", grant.Access)
		}
		canonicalPath := ""
		if err == nil {
			canonicalPath, err = filepath.EvalSymlinks(grant.Path)
		}
		if err == nil && filepath.Clean(canonicalPath) != grant.Path {
			err = fmt.Errorf("path now resolves to %s", filepath.Clean(canonicalPath))
		}
		if err == nil {
			_, err = pathAccess.Grant(grant.Path, grant.Access == WriteAccess)
		}
		if err != nil {
			result.Failures = append(result.Failures, RestoreFailure{Grant: grant, Err: err})
			continue
		}
		current = append(current, grant)
	}

	return &Grants{
		workspaceDir: workspaceDir,
		pathAccess:   pathAccess,
		state:        access.NewRestored(current, canonicalGrants(recordedGrants), definition()),
	}, result
}

func (self *Grants) GetCurrent() []Grant {
	return self.state.GetCurrent()
}

func (self *Grants) Inject() string {
	return self.state.Inject()
}

func (self *Grants) Grant(path string, access Access) (agent.Event, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	if access != ReadAccess && access != WriteAccess {
		return agent.Event{}, fmt.Errorf("access is %q, want read or write", access)
	}
	canonicalPath, err := self.canonicalPath(path, true)
	if err != nil {
		return agent.Event{}, err
	}

	current := self.state.GetCurrent()
	if existing, found := findGrant(current, canonicalPath); found && existing.Access == access {
		return agent.Event{}, fmt.Errorf("%s already has temporary %s access", pathutil.Shorten(canonicalPath), access)
	}
	if _, err := self.pathAccess.Grant(canonicalPath, access == WriteAccess); err != nil {
		return agent.Event{}, fmt.Errorf("could not grant access to %s: %w", pathutil.Shorten(canonicalPath), err)
	}

	current = replaceGrant(current, Grant{Path: canonicalPath, Access: access})
	self.state.Replace(current)
	return ChangeEvent(canonicalPath, current)
}

func (self *Grants) Revoke(path string) (agent.Event, error) {
	self.mutex.Lock()
	defer self.mutex.Unlock()

	canonicalPath, err := self.canonicalPath(path, false)
	if err != nil {
		return agent.Event{}, err
	}
	current := self.state.GetCurrent()
	if _, found := findGrant(current, canonicalPath); !found {
		return agent.Event{}, fmt.Errorf("%s has no temporary grant", pathutil.Shorten(canonicalPath))
	}
	if !self.pathAccess.Revoke(canonicalPath) {
		return agent.Event{}, errors.New("temporary path access was lost before it could be revoked")
	}

	current = slices.DeleteFunc(current, func(grant Grant) bool { return grant.Path == canonicalPath })
	self.state.Replace(current)
	return ChangeEvent(canonicalPath, current)
}

func (self *Grants) canonicalPath(path string, mustExist bool) (string, error) {
	writtenPath := strings.TrimSpace(path)
	if writtenPath == "" {
		return "", errors.New("path is empty")
	}
	path, err := pathutil.Expand(writtenPath)
	if err != nil {
		return "", fmt.Errorf("could not expand %s: %w", writtenPath, err)
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(self.workspaceDir, path)
	}
	path = filepath.Clean(path)

	canonicalPath, err := filepath.EvalSymlinks(path)
	if err == nil {
		return filepath.Clean(canonicalPath), nil
	}
	if mustExist {
		return "", fmt.Errorf("could not resolve %s: %w", pathutil.Shorten(path), err)
	}
	return path, nil
}

func replaceGrant(grants []Grant, replacement Grant) []Grant {
	isReplaced := false
	updatedGrants := make([]Grant, 0, len(grants)+1)
	for _, grant := range grants {
		if grant.Path == replacement.Path {
			updatedGrants = append(updatedGrants, replacement)
			isReplaced = true
		} else {
			updatedGrants = append(updatedGrants, grant)
		}
	}
	if !isReplaced {
		updatedGrants = append(updatedGrants, replacement)
	}
	return canonicalGrants(updatedGrants)
}

func findGrant(grants []Grant, path string) (Grant, bool) {
	for _, grant := range grants {
		if grant.Path == path {
			return grant, true
		}
	}
	return Grant{}, false
}

func canonicalGrants(grants []Grant) []Grant {
	grantsByPath := make(map[string]Grant, len(grants))
	for _, grant := range grants {
		grantsByPath[grant.Path] = grant
	}

	paths := slices.Sorted(maps.Keys(grantsByPath))
	grantList := make([]Grant, 0, len(paths))
	for _, path := range paths {
		grantList = append(grantList, grantsByPath[path])
	}
	return grantList
}

func definition() access.Definition[[]Grant] {
	return access.Definition[[]Grant]{
		Clone:    canonicalGrants,
		Describe: describeChanges,
	}
}

func describeChanges(knownGrants []Grant, currentGrants []Grant) string {
	var clauses []string
	for _, grant := range currentGrants {
		previous, found := findGrant(knownGrants, grant.Path)
		if found && previous.Access == grant.Access {
			continue
		}
		clauses = append(clauses, modelGrantNotice(grant))
	}
	for _, grant := range knownGrants {
		if _, found := findGrant(currentGrants, grant.Path); !found {
			clauses = append(clauses, "The temporary path grant for "+grant.Path+" has been revoked.")
		}
	}
	return strings.Join(clauses, " ")
}

func modelGrantNotice(grant Grant) string {
	if grant.Access == ReadAccess {
		return "The path " + grant.Path + " now has temporary read-only access."
	}
	return "The path " + grant.Path + " now has temporary read-write access when the workspace write capability is granted."
}

const Change agent.Kind = "path_grant_change"

type eventState struct {
	Grants []Grant `json:"grants"`
}

func ChangeEvent(path string, grants []Grant) (agent.Event, error) {
	state, err := json.Marshal(eventState{Grants: canonicalGrants(grants)})
	if err != nil {
		return agent.Event{}, err
	}
	return agent.Event{Kind: Change, Name: path, State: state}, nil
}

func decodeEvent(event agent.Event) ([]Grant, error) {
	var state eventState
	if err := json.Unmarshal(event.State, &state); err != nil {
		return nil, err
	}
	for _, grant := range state.Grants {
		if !filepath.IsAbs(grant.Path) || filepath.Clean(grant.Path) != grant.Path ||
			(grant.Access != ReadAccess && grant.Access != WriteAccess) {
			return nil, errors.New("invalid path grant state")
		}
	}
	return canonicalGrants(state.Grants), nil
}

func LastRecorded(events []agent.Event) ([]Grant, bool) {
	return access.LastRecorded(events, Change, decodeEvent)
}

func Summary(event agent.Event) (string, bool) {
	if event.Kind != Change {
		return "", false
	}
	grants, err := decodeEvent(event)
	if err != nil {
		return "", false
	}
	if len(grants) == 0 {
		return "none", true
	}
	if len(grants) == 1 {
		return "1 path", true
	}
	return fmt.Sprintf("%d paths", len(grants)), true
}

func Notice(event agent.Event) (string, bool) {
	if event.Kind != Change || event.Name == "" {
		return "", false
	}
	grants, err := decodeEvent(event)
	if err != nil {
		return "", false
	}
	if grant, found := findGrant(grants, event.Name); found {
		if grant.Access == ReadAccess {
			return "Granted temporary read-only access to " + grant.Path + ".", true
		}
		return "Granted temporary write access to " + grant.Path + "; changes follow the workspace write capability.", true
	}
	return "Revoked temporary path access to " + event.Name + ".", true
}
