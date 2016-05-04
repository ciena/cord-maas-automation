package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path"
	"reflect"
	"strings"
	"time"

	maas "github.com/juju/gomaasapi"
	"github.com/kelseyhightower/envconfig"
)

// ConfigSpec the configuration information that will be read from the
//   environment
type ConfigSpec struct {
	ApiKey       string `envconfig:"API_KEY" default:"" short:"API key used when connecting to MAAS"`
	MaasURL      string `envconfig:"MAAS_URL" default:"http://localhost/MAAS" short:"URL on which to connect to MAAS"`
	ApiVersion   string `envconfig:"API_VERSION" default:"1.0" short:"MAAS API version to utilized, current 1.0 only"`
	QueryPeriod  string `envconfig:"QUERY_PERIOD" default:"15s" short:"How long to wait between queries to MAAS"`
	Preview      bool   `default:"false" short:"Output what actions would be taken, but don't take them"`
	Mappings     string `default:"{}" short:"Mapping from NIC MAC address to hostname"`
	AlwaysRename bool   `envconfig:"ALWAYS_RENAME" default:"true" short:"Attempt to rename host at every step"`
	Verbose      bool   `default:"false" short:"Output verbose logging"`
	FilterSpec   string `envconfig:"FILTER_SPEC" default:"{\"hosts\":{\"include\":[\".*\"],\"exclude\":[]},\"zones\":{\"include\":[\"default\"],\"exclude\":[]}}" short:"Filter which nodes to include or exclude"`
}

func showHelp() bool {
	for _, arg := range os.Args {
		switch arg {
		case "help":
			fallthrough
		case "-h":
			fallthrough
		case "--help":
			return true
		}
	}
	return false
}

func usageAndExit(prefix string, spec interface{}) error {
	s := reflect.ValueOf(spec)

	if s.Kind() != reflect.Ptr {
		return envconfig.ErrInvalidSpecification
	}
	s = s.Elem()
	if s.Kind() != reflect.Struct {
		return envconfig.ErrInvalidSpecification
	}

	fmt.Printf("USAGE: %s\n", path.Base(os.Args[0]))
	fmt.Println()
	fmt.Println("  This application is configured via the environment. The following environment")
	fmt.Println("  variables can used specified:")
	fmt.Println()
	t := s.Type()
	for i := 0; i < s.NumField(); i++ {
		field := t.Field(i)
		if field.Tag.Get("ignored") == "true" {
			continue
		}
		alt := field.Tag.Get("envconfig")
		fieldName := field.Name
		if alt != "" {
			fieldName = alt
		}

		if prefix != "" {
			fieldName = strings.ToUpper(fmt.Sprintf("%s_%s", prefix, fieldName))
		} else {
			fieldName = strings.ToUpper(fieldName)
		}
		fmt.Printf("  %s\n", fieldName)
		fmt.Printf("    [description] %s\n", field.Tag.Get("short"))
		fmt.Printf("    [type]        %s\n", field.Type.Name())
		fmt.Printf("    [default]     %s\n", field.Tag.Get("default"))
	}

	fmt.Println()
	return nil
}

// checkError if the given err is not nil, then fatally log the message, else
// return false.
func checkError(err error, message string, v ...interface{}) bool {
	if err != nil {
		log.Fatalf("[error] "+message, v)
	}
	return false
}

// checkWarn if the given err is not nil, then log the message as a warning and
// return true, else return false.
func checkWarn(err error, message string, v ...interface{}) bool {
	if err != nil {
		log.Printf("[warn] "+message, v)
		return true
	}
	return false
}

// fetchNodes do a HTTP GET to the MAAS server to query all the nodes
func fetchNodes(client *maas.MAASObject) ([]MaasNode, error) {
	nodeListing := client.GetSubObject("nodes")
	listNodeObjects, err := nodeListing.CallGet("list", url.Values{})
	if checkWarn(err, "unable to get the list of all nodes: %s", err) {
		return nil, err
	}
	listNodes, err := listNodeObjects.GetArray()
	if checkWarn(err, "unable to get the node objects for the list: %s", err) {
		return nil, err
	}

	var nodes = make([]MaasNode, len(listNodes))
	for index, nodeObj := range listNodes {
		node, err := nodeObj.GetMAASObject()
		if !checkWarn(err, "unable to retrieve object for node: %s", err) {
			nodes[index] = MaasNode{node}
		}
	}
	return nodes, nil
}

func main() {

	var config ConfigSpec
	if showHelp() {
		usageAndExit("maas_automation", &config)
		os.Exit(0)
	}

	err := envconfig.Process("maas_automation", &config)
	if err != nil {
		log.Fatal(err)
	}

	options := ProcessingOptions{
		Preview:      config.Preview,
		Verbose:      config.Verbose,
		AlwaysRename: config.AlwaysRename,
	}

	// Determine the filter, this can either be specified on the the command
	// line as a value or a file reference. If none is specified the default
	// will be used
	if len(config.FilterSpec) > 0 {
		if (config.FilterSpec)[0] == '@' {
			name := os.ExpandEnv((config.FilterSpec)[1:])
			file, err := os.OpenFile(name, os.O_RDONLY, 0)
			checkError(err, "[error] unable to open file '%s' to load the filter : %s", name, err)
			decoder := json.NewDecoder(file)
			err = decoder.Decode(&options.Filter)
			checkError(err, "[error] unable to parse filter configuration from file '%s' : %s", name, err)
		} else {
			err := json.Unmarshal([]byte(config.FilterSpec), &options.Filter)
			checkError(err, "[error] unable to parse filter specification: '%s' : %s", config.FilterSpec, err)
		}
	}

	// Determine the mac to name mapping, this can either be specified on the the command
	// line as a value or a file reference. If none is specified the default
	// will be used
	if len(config.Mappings) > 0 {
		if (config.Mappings)[0] == '@' {
			name := os.ExpandEnv((config.Mappings)[1:])
			file, err := os.OpenFile(name, os.O_RDONLY, 0)
			checkError(err, "[error] unable to open file '%s' to load the mac name mapping : %s", name, err)
			decoder := json.NewDecoder(file)
			err = decoder.Decode(&options.Mappings)
			checkError(err, "[error] unable to parse filter configuration from file '%s' : %s", name, err)
		} else {
			err := json.Unmarshal([]byte(config.Mappings), &options.Mappings)
			checkError(err, "[error] unable to parse mac name mapping: '%s' : %s", config.Mappings, err)
		}
	}

	// Verify the specified period for queries can be converted into a Go duration
	period, err := time.ParseDuration(config.QueryPeriod)
	checkError(err, "[error] unable to parse specified query period duration: '%s': %s", &config.QueryPeriod, err)

	authClient, err := maas.NewAuthenticatedClient(config.MaasURL, config.ApiKey, config.ApiVersion)
	if err != nil {
		checkError(err, "[error] Unable to use specified client key, '%s', to authenticate to the MAAS server: %s", config.ApiKey, err)
	}

	// Create an object through which we will communicate with MAAS
	client := maas.NewMAAS(*authClient)

	// This utility essentially polls the MAAS server for node state and
	// process the node to the next state. This is done by kicking off the
	// process every specified duration. This means that the first processing of
	// nodes will have "period" in the future. This is really not the behavior
	// we want, we really want, do it now, and then do the next one in "period".
	// So, the code does one now.
	nodes, _ := fetchNodes(client)
	ProcessAll(client, nodes, options)

	if !(config.Preview) {
		// Create a ticker and fetch and process the nodes every "period"
		ticker := time.NewTicker(period)
		for t := range ticker.C {
			log.Printf("[info] query server at %s", t)
			nodes, _ := fetchNodes(client)
			ProcessAll(client, nodes, options)
		}
	}
}
