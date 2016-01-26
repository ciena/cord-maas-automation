package main

import (
	"flag"
	"fmt"
	"log"
	"net/url"
	"time"

	maas "github.com/juju/gomaasapi"
)

var apiKey = flag.String("apikey", "", "key with which to access MAAS server")
var maasURL = flag.String("maas", "http://localhost/MAAS", "url over which to access MAAS")
var apiVersion = flag.String("apiVersion", "1.0", "version of the API to access")
var queryPeriod = flag.String("period", "15s", "frequency the MAAS service is polled for node states")
var include = flag.String("include", "^cord-r2-", "constrain by hostname what will be automated")

func checkError(err error, message string, v ...interface{}) {
	if err != nil {
		log.Fatalf(message, v)
	}
}

func readStateFromFile() ([]maas.MAASObject, error) {
	var nodes = make([]maas.MAASObject, 0)
	return nodes, nil
}

func fetchNodes(client *maas.MAASObject) ([]MaasNode, error) {
	nodeListing := client.GetSubObject("nodes")
	listNodeObjects, err := nodeListing.CallGet("list", url.Values{})
	checkError(err, "[error] unable to get the list of all nodes: %s", err)
	listNodes, err := listNodeObjects.GetArray()
	checkError(err, "[error] unable to get the node objects for the list: %s", err)
	fmt.Printf("Got list of %v nodes\n", len(listNodes))

	var nodes = make([]MaasNode, len(listNodes))
	for index, nodeObj := range listNodes {
		node, err := nodeObj.GetMAASObject()
		checkError(err, "[error] unable to retrieve object for node: %s", err)
		nodes[index] = MaasNode{node}
	}
	return nodes, nil
}

func main() {

	flag.Parse()

	period, err := time.ParseDuration(*queryPeriod)
	checkError(err, "[error] unable to parse specified query period duration: '%s': %s", queryPeriod, err)

	authClient, err := maas.NewAuthenticatedClient(*maasURL, *apiKey, *apiVersion)
	if err != nil {
		checkError(err, "[error] Unable to connect and authenticate to the MAAS server: %s", err)
	}
	log.Println("[info] connected and authenticated to the MAAS server")
	client := maas.NewMAAS(*authClient)

	// TODO: read last known state from persistence
	nodes, _ := fetchNodes(client)
	ProcessAll(client, nodes, *include)

	// Get a "starting copy of nodes"
	ticker := time.NewTicker(period)
	for t := range ticker.C {
		log.Printf("[info] query server at %s", t)
		nodes, _ := fetchNodes(client)
		ProcessAll(client, nodes, *include)
	}
}
