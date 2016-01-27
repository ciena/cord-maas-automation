package main

import (
	"fmt"
	"log"
	"net/url"
	"regexp"

	maas "github.com/juju/gomaasapi"
)

// Action how to get from there to here
type Action func(*maas.MAASObject, MaasNode, ProcessingOptions) error

// Transition the map from where i want to be from where i might be
type Transition struct {
	Target  string
	Current string
	Using   Action
}

// ProcessingOptions used to determine on what hosts to operate
type ProcessingOptions struct {
	Filter struct {
		Zones struct {
			Include []string
			Exclude []string
		}
		Hosts struct {
			Include []string
			Exclude []string
		}
	}
	Verbose bool
	Preview bool
}

// Transitions the actual map
//
// Currently this is a hand compiled / optimized "next step" table. This should
// really be generated from the state machine chart input. Once this has been
// accomplished you should be able to determine the action to take given your
// target state and your current state.
var Transitions = map[string]map[string]Action{
	"Deployed": {
		"New":                 Commission,
		"Deployed":            Done,
		"Ready":               Aquire,
		"Allocated":           Deploy,
		"Retired":             AdminState,
		"Reserved":            AdminState,
		"Releasing":           Wait,
		"DiskErasing":         Wait,
		"Deploying":           Wait,
		"Commissioning":       Wait,
		"Missing":             Fail,
		"FailedReleasing":     Fail,
		"FailedDiskErasing":   Fail,
		"FailedDeployment":    Fail,
		"Broken":              Fail,
		"FailedCommissioning": Fail,
	},
}

const (
	// defaultStateMachine Would be nice to drive from a graph language
	defaultStateMachine string = `
        (New)->(Commissioning)
        (Commissioning)->(FailedCommissioning)
        (FailedCommissioning)->(New)
        (Commissioning)->(Ready)
        (Ready)->(Deploying)
        (Ready)->(Allocated)
        (Allocated)->(Deploying)
        (Deploying)->(Deployed)
        (Deploying)->(FailedDeployment)
        (FailedDeployment)->(Broken)
        (Deployed)->(Releasing)
        (Releasing)->(FailedReleasing)
        (FailedReleasing)->(Broken)
        (Releasing)->(DiskErasing)
        (DiskErasing)->(FailedEraseDisk)
        (FailedEraseDisk)->(Broken)
        (Releasing)->(Ready)
        (DiskErasing)->(Ready)
        (Broken)->(Ready)`
)

// Done we are at the target state, nothing to do
var Done = func(client *maas.MAASObject, node MaasNode, options ProcessingOptions) error {
	log.Printf("COMPLETE: %s", node.Hostname())
	return nil
}

// Deploy cause a node to deploy
var Deploy = func(client *maas.MAASObject, node MaasNode, options ProcessingOptions) error {
	log.Printf("DEPLOY: %s", node.Hostname())
	if !options.Preview {
		nodesObj := client.GetSubObject("nodes")
		myNode := nodesObj.GetSubObject(node.ID())
		_, err := myNode.CallPost("start", nil)
		if err != nil {
			return err
		}
	}
	return nil
}

// Aquire aquire a machine to a specific operator
var Aquire = func(client *maas.MAASObject, node MaasNode, options ProcessingOptions) error {
	log.Printf("AQUIRE: %s", node.Hostname())
	if !options.Preview {
		nodesObj := client.GetSubObject("nodes")
		params := url.Values{"name": []string{node.Hostname()}}
		_, err := nodesObj.CallPost("acquire", params)
		if err != nil {
			return err
		}
	}
	return nil
}

// Commission cause a node to be commissioned
var Commission = func(client *maas.MAASObject, node MaasNode, options ProcessingOptions) error {
	log.Printf("COMISSION: %s", node.Hostname())
	if !options.Preview {

		nodesObj := client.GetSubObject("nodes")
		nodeObj := nodesObj.GetSubObject(node.ID())
		_, err := nodeObj.CallPost("commission", url.Values{})
		if err != nil {
			return err
		}
	}
	return nil
}

// Wait a do nothing state, while work is being done
var Wait = func(client *maas.MAASObject, node MaasNode, options ProcessingOptions) error {
	log.Printf("WAIT: %s", node.Hostname())
	return nil
}

// Fail a state from which we cannot, currently, automatically recover
var Fail = func(client *maas.MAASObject, node MaasNode, options ProcessingOptions) error {
	log.Printf("FAIL: %s", node.Hostname())
	return nil
}

// AdminState an administrative state from which we should make no automatic transition
var AdminState = func(client *maas.MAASObject, node MaasNode, options ProcessingOptions) error {
	log.Printf("ADMIN: %s", node.Hostname())
	return nil
}

func findAction(target string, current string) (Action, error) {
	targets, ok := Transitions[target]
	if !ok {
		log.Printf("[warn] unable to find transitions to target state '%s'", target)
		return nil, fmt.Errorf("Could not find transition to target state '%s'", target)
	}

	action, ok := targets[current]
	if !ok {
		log.Printf("[warn] unable to find transition from current state '%s' to target state '%s'",
			current, target)
		return nil, fmt.Errorf("Could not find transition from current state '%s' to target state '%s'",
			current, target)
	}

	return action, nil
}

// ProcessNode something
func ProcessNode(client *maas.MAASObject, node MaasNode, options ProcessingOptions) error {
	substatus, err := node.GetInteger("substatus")
	if err != nil {
		return err
	}
	action, err := findAction("Deployed", MaasNodeStatus(substatus).String())
	if err != nil {
		return err
	}

	if options.Preview {
		action(client, node, options)
	} else {
		go action(client, node, options)
	}
	return nil
}

func buildFilter(filter []string) ([]*regexp.Regexp, error) {

	results := make([]*regexp.Regexp, len(filter))
	for i, v := range filter {
		r, err := regexp.Compile(v)
		if err != nil {
			return nil, err
		}
		results[i] = r
	}
	return results, nil
}

func matchedFilter(include []*regexp.Regexp, target string) bool {
	for _, e := range include {
		if e.MatchString(target) {
			return true
		}
	}
	return false
}

// ProcessAll something
func ProcessAll(client *maas.MAASObject, nodes []MaasNode, options ProcessingOptions) []error {
	errors := make([]error, len(nodes))
	includeHosts, err := buildFilter(options.Filter.Hosts.Include)
	if err != nil {
		log.Fatalf("[error] invalid regular expression for include filter '%s' : %s", options.Filter.Hosts.Include, err)
	}

	includeZones, err := buildFilter(options.Filter.Zones.Include)
	if err != nil {
		log.Fatalf("[error] invalid regular expression for include filter '%v' : %s", options.Filter.Zones.Include, err)
	}

	for i, node := range nodes {
		// For hostnames we always match on an empty filter
		if len(includeHosts) >= 0 && matchedFilter(includeHosts, node.Hostname()) {

			// For zones we don't match on an empty filter
			if len(includeZones) >= 0 && matchedFilter(includeZones, node.Zone()) {
				err := ProcessNode(client, node, options)
				if err != nil {
					errors[i] = err
				} else {
					errors[i] = nil
				}
			} else {
				if options.Verbose {
					log.Printf("[info] ignoring node '%s' as its zone '%s' didn't match include zone name filter '%v'",
						node.Hostname(), node.Zone(), options.Filter.Zones.Include)
				}
			}
		} else {
			if options.Verbose {
				log.Printf("[info] ignoring node '%s' as it didn't match include hostname filter '%v'",
					node.Hostname(), options.Filter.Hosts.Include)
			}
		}
	}
	return errors
}
