package main

import (
	"fmt"
	"log"
	"net/url"
	"regexp"

	maas "github.com/juju/gomaasapi"
)

// Action how to get from there to here
type Action func(*maas.MAASObject, MaasNode) error

// Transition the map from where i want to be from where i might be
type Transition struct {
	Target  string
	Current string
	Using   Action
}

// Filter used to determine on what hosts to operate
type Filter struct {
	includeZones []string
	includeNames []string
}

// Transitions the actual map
//
// Currently this is a hand compiled / optimized "next step" table. This should
// really be generated from the state machine chart input. Once this has been
// accomplished you should be able to determine the action to take given your
// target state and your current state.
var Transitions = []Transition{
	{"Deployed", "Deployed", Done},
	{"Deployed", "Ready", Aquire},
	{"Deployed", "Allocated", Deploy},
	{"Deployed", "Retired", AdminState},
	{"Deployed", "Reserverd", AdminState},
	{"Deployed", "Releasing", Wait},
	{"Deployed", "DiskErasing", Wait},
	{"Deployed", "Deploying", Wait},
	{"Deployed", "Commissioning", Wait},
	{"Deployed", "Missing", Fail},
	{"Deployed", "FailedReleasing", Fail},
	{"Deployed", "FailedDiskErasing", Fail},
	{"Deployed", "FailedDeployment", Fail},
	{"Deployed", "Broken", Fail},
	{"Deployed", "FailedCommissioning", Fail},

	{"Deployed", "New", Comission},
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
var Done = func(client *maas.MAASObject, node MaasNode) error {
	log.Printf("COMPLETE: %s", node.Hostname())
	return nil
}

// Deploy cause a node to deploy
var Deploy = func(client *maas.MAASObject, node MaasNode) error {
	log.Printf("DEPLOY: %s", node.Hostname())
	nodesObj := client.GetSubObject("nodes")
	myNode := nodesObj.GetSubObject(node.ID())
	_, err := myNode.CallPost("start", nil)
	if err != nil {
		return err
	}
	return nil
}

// Aquire aquire a machine to a specific operator
var Aquire = func(client *maas.MAASObject, node MaasNode) error {
	log.Printf("AQUIRE: %s", node.Hostname())
	nodesObj := client.GetSubObject("nodes")
	params := url.Values{"name": []string{node.Hostname()}}
	_, err := nodesObj.CallPost("acquire", params)
	if err != nil {
		return err
	}
	return nil
}

// Comission cause a node to be commissioned
var Comission = func(client *maas.MAASObject, node MaasNode) error {
	log.Printf("COMISSION: %s", node.Hostname())
	nodesObj := client.GetSubObject("nodes")
	nodeObj := nodesObj.GetSubObject(node.ID())
	_, err := nodeObj.CallPost("commission", url.Values{})
	if err != nil {
		return err
	}
	return nil
}

// Wait a do nothing state, while work is being done
var Wait = func(client *maas.MAASObject, node MaasNode) error {
	log.Printf("WAIT: %s", node.Hostname())
	return nil
}

// Fail a state from which we cannot, currently, automatically recover
var Fail = func(client *maas.MAASObject, node MaasNode) error {
	log.Printf("FAIL: %s", node.Hostname())
	return nil
}

// AdminState an administrative state from which we should make no automatic transition
var AdminState = func(client *maas.MAASObject, node MaasNode) error {
	log.Printf("ADMIN: %s", node.Hostname())
	return nil
}

func findAction(target string, current string) (Action, error) {
	for _, t := range Transitions {
		if t.Current == current {
			return t.Using, nil
		}
	}
	return nil, fmt.Errorf("Could not find transition from current state '%s'", current)
}

// ProcessNode something
func ProcessNode(client *maas.MAASObject, node MaasNode) error {
	substatus, err := node.GetInteger("substatus")
	if err != nil {
		return err
	}

	action, err := findAction("", MaasNodeStatus(substatus).String())
	if err != nil {
		return err
	}
	go action(client, node)
	return nil
}

func buildHostNameFilter(filter interface{}) ([]*regexp.Regexp, error) {
	hosts, ok := filter.(map[string]interface{})["hosts"]
	if !ok {
		return []*regexp.Regexp{}, nil
	}

	include, ok := hosts.(map[string]interface{})["include"]
	if !ok {
		return []*regexp.Regexp{}, nil
	}

	values := include.([]interface{})
	results := make([]*regexp.Regexp, len(values))
	for i, v := range values {
		r, err := regexp.Compile(v.(string))
		if err != nil {
			return nil, err
		}
		results[i] = r
	}
	return results, nil
}

func buildZoneFilter(filter interface{}) ([]*regexp.Regexp, error) {
	zones, ok := filter.(map[string]interface{})["zones"]
	if !ok {
		return []*regexp.Regexp{}, nil
	}

	include, ok := zones.(map[string]interface{})["include"]
	if !ok {
		return []*regexp.Regexp{}, nil
	}

	values := include.([]interface{})
	results := make([]*regexp.Regexp, len(values))
	for i, v := range values {
		r, err := regexp.Compile(v.(string))
		if err != nil {
			return nil, err
		}
		results[i] = r
	}
	return results, nil
}

func matchedZoneFilter(include []*regexp.Regexp, zone string) bool {
	for _, e := range include {
		if e.MatchString(zone) {
			return true
		}
	}
	return false
}

func matchedHostnameFilter(include []*regexp.Regexp, hostname string) bool {
	for _, e := range include {
		if e.MatchString(hostname) {
			return true
		}
	}
	return false
}

// ProcessAll something
func ProcessAll(client *maas.MAASObject, nodes []MaasNode, filter interface{}) []error {
	errors := make([]error, len(nodes))
	includeHosts, err := buildHostNameFilter(filter)
	if err != nil {
		log.Fatalf("[error] invalid regular expression for include filter '%s' : %s", filter, err)
	}

	includeZones, err := buildZoneFilter(filter)
	if err != nil {
		log.Fatalf("[error] invalid regular expression for include filter '%s' : %s", filter, err)
	}

	for i, node := range nodes {
		if matchedHostnameFilter(includeHosts, node.Hostname()) && matchedZoneFilter(includeZones, node.Zone()) {
			err := ProcessNode(client, node)
			if err != nil {
				errors[i] = err
			} else {
				errors[i] = nil
			}
		} else {
			log.Printf("[info] ignoring node '%s' as it didn't match include filter '%s'", node.Hostname(), filter)
		}
	}
	return errors
}
