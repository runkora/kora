package githubapp

import (
	"maps"
	"net/http"
	"slices"
	"strings"

	"github.com/runkora/kora/backend/internal/credentials"
)

// restRoute is one REST endpoint from GitHub's published permission data
// (routes_gen.go). Any one of Sets, held in full, authorizes the call; an
// endpoint with no sets needs only metadata. Refused marks an endpoint
// installation tokens cannot call: it stays in the table so a request for it
// never falls through to a broader template.
type restRoute struct {
	Method  string
	Path    string
	Sets    []permissionSet
	Refused bool
}

// permissionSet maps GitHub App permission keys to an access level.
type permissionSet map[string]string

// categoryPermissions groups the GitHub App permissions into the connection's
// categories. A permission outside every category is never minted. See
// docs/adr/2026-11-15-github-connector-route-coverage.md.
var categoryPermissions = map[string][]string{
	catContents:    {"contents"},
	catWorkflows:   {"workflows"},
	catIssues:      {"issues"},
	catPulls:       {"pull_requests"},
	catCI:          {"actions", "checks", "statuses", "artifact_metadata", "attestations"},
	catDeployments: {"deployments", "environments", "pages"},
	catSecurity:    {"security_events", "vulnerability_alerts", "secret_scanning_alerts", "repository_advisories", "code_quality"},
	catSecrets:     {"secrets", "actions_variables", "dependabot_secrets", "agent_secrets", "agent_variables"},
	catAdmin:       {"administration", "repository_hooks", "repository_custom_properties", "repository_creation", "installation_repositories"},
	catMembers:     {"members"},
	catOrgProjects: {"organization_projects"},
}

// permissionCategory is the reverse of categoryPermissions.
var permissionCategory = func() map[string]string {
	m := map[string]string{}
	for category, perms := range categoryPermissions {
		for _, p := range perms {
			m[p] = category
		}
	}
	return m
}()

// greedyParams are the path parameters whose values contain slashes. They
// span several segments, and only as the last segment of a template.
var greedyParams = map[string]bool{"{path}": true, "{ref}": true, "{branch}": true, "{basehead}": true}

// compiledRoute is a restRoute split into segments; an empty segment is a
// parameter.
type compiledRoute struct {
	route  *restRoute
	segs   []string
	greedy bool
}

var routesByMethod = func() map[string][]compiledRoute {
	m := map[string][]compiledRoute{}
	for i := range restRoutes {
		r := &restRoutes[i]
		segs := pathSegments(r.Path)
		c := compiledRoute{route: r, segs: make([]string, len(segs))}
		for j, s := range segs {
			if !strings.HasPrefix(s, "{") {
				c.segs[j] = s
			}
		}
		c.greedy = len(segs) > 0 && greedyParams[segs[len(segs)-1]]
		m[r.Method] = append(m[r.Method], c)
	}
	return m
}()

// matchRoute returns the most specific route for the escaped path segments: at
// the first segment where candidates differ a literal beats a parameter, and
// an exact route beats a greedy one.
func matchRoute(method string, segs []string) *restRoute {
	var best *compiledRoute
	bestScore := -1
	for i := range routesByMethod[method] {
		c := &routesByMethod[method][i]
		score, ok := c.match(segs)
		if ok && score > bestScore {
			best, bestScore = c, score
		}
	}
	if best == nil {
		return nil
	}
	return best.route
}

// maxRouteSegments bounds the scoring below; GitHub's longest template is
// well under it.
const maxRouteSegments = 30

// match reports whether segs fit the route. A greedy parameter may also be
// empty, as in a listing of the repository root's contents. The score weights a
// literal by its position, so an earlier literal outweighs every later one, and
// adds one for an exact route.
func (c *compiledRoute) match(segs []string) (int, bool) {
	if len(segs) != len(c.segs) && (!c.greedy || len(segs) < len(c.segs)-1) {
		return 0, false
	}
	score := 0
	for i, s := range c.segs {
		if i == len(segs) {
			break // the empty greedy parameter
		}
		switch s {
		case "":
		case segs[i]:
			score += 2 << (maxRouteSegments - i)
		default:
			return 0, false
		}
	}
	if !c.greedy {
		score++
	}
	return score, true
}

// routeClassification reduces a route's permission sets to a classification.
// A superset of another set is dropped. Sets of one category each become the
// action and its alternates; otherwise the first set's categories are all
// required. An endpoint GitHub serves with metadata alone needs only the
// baseline. ok is false when no set falls within the categories, and for a
// write that needs only metadata, which the baseline must not authorize.
func routeClassification(r *restRoute) (credentials.Classification, bool) {
	if r.Refused {
		return credentials.Classification{}, false
	}
	metadataOnly := credentials.Classification{Action: ActionMetadataRead, Permissions: map[string]map[string]string{
		ActionMetadataRead: {catMetadata: verbRead},
	}}
	if len(r.Sets) == 0 {
		return metadataOnly, r.Method == http.MethodGet
	}
	var sets []map[string]permissionSet
	for _, set := range r.Sets {
		grouped, ok := groupByCategory(set)
		if _, baseline := grouped[ActionMetadataRead]; ok && baseline {
			return metadataOnly, r.Method == http.MethodGet
		}
		if ok {
			sets = append(sets, grouped)
		}
	}
	sets = minimalSets(sets)
	if len(sets) == 0 {
		return credentials.Classification{}, false
	}

	var singles []map[string]permissionSet
	for _, s := range sets {
		if len(s) == 1 {
			singles = append(singles, s)
		}
	}
	cls := credentials.Classification{Permissions: map[string]map[string]string{}}
	if len(singles) > 0 {
		for _, s := range singles {
			for _, action := range slices.Sorted(maps.Keys(s)) {
				addAction(&cls, action, s[action], &cls.Alternates)
			}
		}
		return cls, true
	}
	for _, action := range slices.Sorted(maps.Keys(sets[0])) {
		addAction(&cls, action, sets[0][action], &cls.AlsoRequired)
	}
	return cls, true
}

// addAction makes action the classification's action, or appends it to rest,
// once, with the permissions it mints.
func addAction(cls *credentials.Classification, action string, perms permissionSet, rest *[]string) {
	if _, ok := cls.Permissions[action]; ok {
		return
	}
	cls.Permissions[action] = perms
	if cls.Action == "" {
		cls.Action = action
		return
	}
	*rest = append(*rest, action)
}

// groupByCategory turns a permission set into its category actions, each with
// the permissions it mints. Metadata is the baseline, so a set of metadata
// alone is metadata:read. ok is false when a permission is in no category.
func groupByCategory(set permissionSet) (map[string]permissionSet, bool) {
	out := map[string]permissionSet{}
	verbs := map[string]string{}
	for perm, level := range set {
		if perm == catMetadata {
			continue
		}
		category, ok := permissionCategory[perm]
		if !ok {
			return nil, false
		}
		if verbs[category] != verbWrite {
			verbs[category] = verbRead
			if level != verbRead {
				verbs[category] = verbWrite
			}
		}
		if out[category] == nil {
			out[category] = permissionSet{}
		}
		out[category][perm] = level
	}
	if len(out) == 0 {
		return map[string]permissionSet{ActionMetadataRead: {catMetadata: verbRead}}, true
	}
	actions := map[string]permissionSet{}
	for category, perms := range out {
		actions[category+":"+verbs[category]] = perms
	}
	return actions, true
}

// minimalSets drops every set that contains another, and repeats of a set.
func minimalSets(sets []map[string]permissionSet) []map[string]permissionSet {
	var out []map[string]permissionSet
	for i, s := range sets {
		keep := true
		for j, other := range sets {
			if i != j && covers(s, other) && (!covers(other, s) || j < i) {
				keep = false
				break
			}
		}
		if keep {
			out = append(out, s)
		}
	}
	return out
}

// covers reports whether every permission of b is in a at the same level.
func covers(a, b map[string]permissionSet) bool {
	for _, bperms := range b {
		for perm, level := range bperms {
			found := false
			for _, aperms := range a {
				if aperms[perm] == level {
					found = true
				}
			}
			if !found {
				return false
			}
		}
	}
	return true
}
