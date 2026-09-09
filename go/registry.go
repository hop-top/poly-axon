package axon

import (
	"io/fs"
	"sort"
	"sync"
)

var (
	hostsOnce   sync.Once
	hostsByName map[string]Host
	aliasIndex  map[string]string
	hostsErr    error
)

func loadHosts() {
	hostsByName = map[string]Host{}
	aliasIndex = map[string]string{}
	entries, err := fs.ReadDir(Spec(), "hosts")
	if err != nil {
		hostsErr = err
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		var h Host
		if err := loadYAML(Spec(), "hosts/"+e.Name()+"/host.yaml", "host.schema.json", &h); err != nil {
			hostsErr = err
			return
		}
		hostsByName[h.Name] = h
		for _, a := range h.Aliases {
			aliasIndex[a] = h.Name
		}
	}
}

func ensureHosts() {
	hostsOnce.Do(loadHosts)
	if hostsErr != nil {
		panic(hostsErr)
	}
}

// Hosts returns every host sorted by canonical name.
func Hosts() []Host {
	ensureHosts()
	out := make([]Host, 0, len(hostsByName))
	for _, h := range hostsByName {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Get looks up a host by canonical name only.
func Get(name string) (Host, bool) {
	ensureHosts()
	h, ok := hostsByName[name]
	return h, ok
}

// Resolve accepts a canonical name or a published alias. It is the only
// alias-aware function in the module.
func Resolve(nameOrAlias string) (Host, bool) {
	ensureHosts()
	if h, ok := hostsByName[nameOrAlias]; ok {
		return h, true
	}
	if canon, ok := aliasIndex[nameOrAlias]; ok {
		return hostsByName[canon], true
	}
	return Host{}, false
}

// HookedHosts returns hosts with a hook surface (hooks: true).
func HookedHosts() []Host {
	var out []Host
	for _, h := range Hosts() {
		if h.Hooks {
			out = append(out, h)
		}
	}
	return out
}
